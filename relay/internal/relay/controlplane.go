package relay

// controlplane.go — pivot Faz 0 minimal control plane (/v1/apps).
//
// v0 scope: create an app + seed a kind='cloud' route (so a subdomain resolves
// to a persistent container), list apps, delete an app. This is enough to wire
// the tracer bullet (a manually-started hello-world container) end to end.
//
// The full deploy pipeline (tar upload -> buildd -> DockerDriver.Deploy ->
// route write with the real container endpoint + SSE build logs) builds on top
// of this in the next stage. Auth reuses the existing CLI login JWT.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ahmetvural79/tunr/relay/internal/auth"
	"github.com/ahmetvural79/tunr/relay/internal/db"
	"github.com/ahmetvural79/tunr/relay/internal/logger"
)

// ControlPlane serves the /v1/apps REST surface.
type ControlPlane struct {
	jwt    *auth.JWTAuth
	db     *db.DB
	domain string
}

// NewControlPlane builds the control plane. database may be nil (in-memory mode);
// endpoints then return 503 since apps require persistence.
func NewControlPlane(jwtAuth *auth.JWTAuth, database *db.DB, domain string) *ControlPlane {
	return &ControlPlane{jwt: jwtAuth, db: database, domain: domain}
}

// RegisterRoutes mounts /v1/apps on the mux.
func (c *ControlPlane) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/apps", c.handleApps)
}

// authUser extracts and verifies the Bearer JWT, returning the user id.
func (c *ControlPlane) authUser(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return "", false
	}
	claims, err := c.jwt.Verify(strings.TrimPrefix(h, "Bearer "))
	if err != nil || claims.UserID == "" {
		return "", false
	}
	return claims.UserID, true
}

type createAppReq struct {
	Name         string `json:"name"`
	InternalPort int    `json:"internal_port"`
	CloudURL     string `json:"cloud_url"` // optional v0 override (manual tracer bullet)
}

type appResp struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	CloudURL string `json:"cloud_url"`
	Status   string `json:"status"`
}

func (c *ControlPlane) handleApps(w http.ResponseWriter, r *http.Request) {
	if c.db == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "apps require a database (relay is in in-memory mode)")
		return
	}
	userID, ok := c.authUser(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "valid Bearer token required (run: tunr login)")
		return
	}

	switch r.Method {
	case http.MethodPost:
		c.createApp(w, r, userID)
	case http.MethodGet:
		writeJSONError(w, http.StatusNotImplemented, "app listing arrives with the full deploy pipeline")
	case http.MethodDelete:
		c.deleteApp(w, r, userID)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (c *ControlPlane) createApp(w http.ResponseWriter, r *http.Request, userID string) {
	var req createAppReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Name = strings.ToLower(strings.TrimSpace(req.Name))
	if !validSubdomain(req.Name) {
		writeJSONError(w, http.StatusBadRequest, "name must be a valid subdomain (a-z, 0-9, dash; 1-63 chars)")
		return
	}
	if req.InternalPort == 0 {
		req.InternalPort = 8080
	}

	// Name collision (unique) → 409.
	if _, exists, err := c.db.GetAppByName(r.Context(), req.Name); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	} else if exists {
		writeJSONError(w, http.StatusConflict, fmt.Sprintf("name %q is taken", req.Name))
		return
	}

	id := newAppID()
	edgeSecret := newEdgeSecret()
	cloudURL := req.CloudURL
	if cloudURL == "" {
		// own-server default: relay reaches the app container by name on tunr-apps.
		cloudURL = fmt.Sprintf("http://tunr-app-%s:%d", id, req.InternalPort)
	}

	app := db.App{
		ID:           id,
		UserID:       userID,
		Name:         req.Name,
		Region:       "ams",
		InternalPort: req.InternalPort,
		EdgeSecret:   edgeSecret,
		Status:       "live",
	}
	if err := c.db.CreateApp(r.Context(), app); err != nil {
		logger.Warn("createApp %s: %v", req.Name, err)
		writeJSONError(w, http.StatusInternalServerError, "could not create app")
		return
	}
	if err := c.db.UpsertCloudRoute(r.Context(), req.Name, id, cloudURL, 30); err != nil {
		logger.Warn("upsert route %s: %v", req.Name, err)
		// Best-effort cleanup so we don't leave an app without a route.
		_ = c.db.DeleteApp(r.Context(), id)
		writeJSONError(w, http.StatusInternalServerError, "could not create route")
		return
	}

	writeJSON(w, http.StatusCreated, appResp{
		ID:       id,
		Name:     req.Name,
		URL:      fmt.Sprintf("https://%s.%s", req.Name, c.domain),
		CloudURL: cloudURL,
		Status:   "live",
	})
}

func (c *ControlPlane) deleteApp(w http.ResponseWriter, r *http.Request, userID string) {
	name := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("name")))
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "?name= required")
		return
	}
	app, exists, err := c.db.GetAppByName(r.Context(), name)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !exists {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("no app named %q", name))
		return
	}
	if app.UserID != userID {
		// Don't leak existence to other users.
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("no app named %q", name))
		return
	}
	// FK cascade removes the route + deployments; NOTIFY drops it from the relay cache.
	if err := c.db.DeleteApp(r.Context(), app.ID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": name})
}

// ---------- helpers ----------

func validSubdomain(s string) bool {
	if len(s) == 0 || len(s) > 63 {
		return false
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, ch := range s {
		if !(ch >= 'a' && ch <= 'z') && !(ch >= '0' && ch <= '9') && ch != '-' {
			return false
		}
	}
	return true
}

func newAppID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return "a_" + hex.EncodeToString(b)
}

func newEdgeSecret() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
