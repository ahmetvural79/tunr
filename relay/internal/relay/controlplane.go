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
	"strconv"
	"strings"

	"github.com/ahmetvural79/tunr/relay/internal/auth"
	"github.com/ahmetvural79/tunr/relay/internal/db"
	"github.com/ahmetvural79/tunr/relay/internal/logger"
)

// ControlPlane serves the /v1/apps + /v1/deploy REST surface.
type ControlPlane struct {
	jwt    *auth.JWTAuth
	db     *db.DB
	domain string
	runner *RunnerClient
	// sched resolves which node holds an app. Per-app reads go through it
	// rather than through runner directly — see scheduler.go.
	sched *Scheduler
}

// NewControlPlane builds the control plane. database may be nil (in-memory mode);
// endpoints then return 503 since apps require persistence.
func NewControlPlane(jwtAuth *auth.JWTAuth, database *db.DB, domain string, runner *RunnerClient, sched *Scheduler) *ControlPlane {
	return &ControlPlane{jwt: jwtAuth, db: database, domain: domain, runner: runner, sched: sched}
}

// RegisterRoutes mounts the control-plane endpoints on the mux.
//
// "/v1/apps" is an exact-match pattern, so "/v1/apps/logs" needs its own
// registration — it does not fall through to handleApps.
func (c *ControlPlane) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/apps", c.handleApps)
	mux.HandleFunc("/v1/apps/logs", c.handleAppLogs)
	mux.HandleFunc("/v1/deploy", c.handleDeploy)
}

// handleAppLogs streams an app's runtime output to its owner.
//
//	GET /v1/apps/logs?name=my-app&tail=200&follow=1   (Bearer JWT)
func (c *ControlPlane) handleAppLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if c.db == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "logs require a database")
		return
	}
	userID, ok := c.authUser(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "valid Bearer token required (run: tunr login)")
		return
	}
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
	// Ownership check doubles as existence check — never confirm to a stranger
	// that a name is taken.
	if !exists || app.UserID != userID {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("no app named %q", name))
		return
	}
	if c.sched == nil || !c.sched.Enabled() {
		writeJSONError(w, http.StatusServiceUnavailable, "logs are not available (no runner configured)")
		return
	}

	tail := 200
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, convErr := strconv.Atoi(v); convErr == nil && n >= 0 && n <= 5000 {
			tail = n
		}
	}
	follow := r.URL.Query().Get("follow") == "1"

	rc, err := c.sched.Logs(r.Context(), app.ID, tail, follow)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "could not read logs: "+err.Error())
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32<<10)
	for {
		n, rerr := rc.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			return
		}
	}
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
		c.listApps(w, r, userID)
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

func (c *ControlPlane) listApps(w http.ResponseWriter, r *http.Request, userID string) {
	rows, err := c.db.ListAppsByUser(r.Context(), userID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not list apps")
		return
	}
	apps := make([]appResp, 0, len(rows))
	for _, a := range rows {
		apps = append(apps, appResp{
			ID:     a.ID,
			Name:   a.Name,
			URL:    fmt.Sprintf("https://%s.%s", a.Name, c.domain),
			Status: a.Status,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": apps})
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

func newDeploymentID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return "dep_" + hex.EncodeToString(b)
}

// handleDeploy: build + run a project uploaded by the CLI, then route its
// subdomain to the resulting container. Streams SSE back to the CLI.
//
//	POST /v1/deploy  (Bearer JWT)  multipart: meta{name,internal_port,env} + source(tar.gz)
func (c *ControlPlane) handleDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if c.db == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "deploy requires a database")
		return
	}
	if c.runner == nil || !c.runner.Enabled() {
		writeJSONError(w, http.StatusServiceUnavailable, "deploy is not available (no runner configured)")
		return
	}
	userID, ok := c.authUser(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "valid Bearer token required (run: tunr login)")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}
	fail := func(msg string) { send(map[string]string{"event": "failed", "error": msg}) }

	r.Body = http.MaxBytesReader(w, r.Body, 55<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		fail("bad multipart: " + err.Error())
		return
	}
	var meta struct {
		Name         string            `json:"name"`
		InternalPort int               `json:"internal_port"`
		Env          map[string]string `json:"env"`
	}
	if err := json.Unmarshal([]byte(r.FormValue("meta")), &meta); err != nil {
		fail("bad meta json")
		return
	}
	meta.Name = strings.ToLower(strings.TrimSpace(meta.Name))
	if !validSubdomain(meta.Name) {
		fail("name must be a valid subdomain (a-z, 0-9, dash)")
		return
	}
	if meta.InternalPort == 0 {
		meta.InternalPort = 8080
	}
	src, _, err := r.FormFile("source")
	if err != nil {
		fail("missing source file")
		return
	}
	defer src.Close()

	ctx := r.Context()

	app, _, err := c.db.GetOrCreateAppByName(ctx, userID, meta.Name, newAppID(), newEdgeSecret(), meta.InternalPort)
	if err != nil {
		fail(err.Error())
		return
	}

	seq, err := c.db.NextDeploymentSeq(ctx, app.ID)
	if err != nil {
		fail("deployment seq: " + err.Error())
		return
	}
	depID := newDeploymentID()
	_ = c.db.InsertDeployment(ctx, depID, app.ID, seq, "building")

	runnerMeta, _ := json.Marshal(map[string]any{
		"app_id":        app.ID,
		"name":          app.Name,
		"deployment_id": depID,
		"internal_port": app.InternalPort,
		"edge_secret":   app.EdgeSecret,
		"env":           meta.Env,
		"memory_mb":     256,
		"cpus":          1.0,
	})

	send(map[string]string{"event": "queued", "detail": "sending to builder"})
	endpoint, err := c.runner.Deploy(ctx, runnerMeta, src, func(ev map[string]any) { send(ev) })
	if err != nil {
		_ = c.db.UpdateDeployment(ctx, depID, "failed", "", err.Error())
		fail(err.Error())
		return
	}

	if err := c.db.UpsertCloudRoute(ctx, app.Name, app.ID, endpoint, 30); err != nil {
		fail("route: " + err.Error())
		return
	}
	_ = c.db.SetAppStatus(ctx, app.ID, "live")
	_ = c.db.UpdateDeployment(ctx, depID, "healthy", "", "")

	send(map[string]any{
		"event": "live",
		"url":   fmt.Sprintf("https://%s.%s", app.Name, c.domain),
		"seq":   seq,
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
