// tunr-runner — the sidecar that actually drives Docker.
//
// It runs as a small privileged container with the Docker socket mounted and a
// leg on the tunr-apps network. The relay (scratch, no Docker access) and the
// control plane call this HTTP API instead of touching Docker directly:
//
//	POST   /v1/deploy          multipart: meta(json) + source(tar.gz)
//	                           -> SSE: build logs, then {"event":"ready","endpoint":"http://IP:PORT"}
//	POST   /v1/apps/{id}/wake  -> 200 once dialable (or best-effort)
//	POST   /v1/apps/{id}/sleep
//	POST   /v1/apps/{id}/stop
//	DELETE /v1/apps/{id}
//	GET    /v1/apps/{id}/status  -> {"status":"running|sleeping|stopped"}
//
// All requests authenticate with a shared bearer secret (RUNNER_SECRET).
// Build = Nixpacks (or Dockerfile); Run = runner.DockerDriver (gVisor, quotas).
package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/subtle"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ahmetvural79/tunr/relay/internal/runner"
)

const (
	maxSourceBytes  = 50 << 20
	maxExtractBytes = 500 << 20
	buildTimeout    = 15 * time.Minute
)

var (
	listen  = flag.String("listen", ":9091", "listen address")
	workdir = flag.String("workdir", "/work", "scratch dir for extracted sources")
	secret  = flag.String("secret", os.Getenv("RUNNER_SECRET"), "bearer auth shared secret")
	runtime = flag.String("runtime", envOr("TUNR_DOCKER_RUNTIME", "runsc"), "docker runtime for app containers")

	drv     *runner.DockerDriver
	buildMu sync.Mutex // one build at a time (v0)
)

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// DeployMeta is the JSON side of the /v1/deploy multipart upload.
type DeployMeta struct {
	AppID        string            `json:"app_id"`
	Name         string            `json:"name"`
	DeploymentID string            `json:"deployment_id"`
	InternalPort int               `json:"internal_port"`
	EdgeSecret   string            `json:"edge_secret"`
	Env          map[string]string `json:"env"`
	MemoryMB     int               `json:"memory_mb"`
	CPUs         float64           `json:"cpus"`
	NoCache      bool              `json:"no_cache"`
}

func main() {
	flag.Parse()
	if *secret == "" {
		log.Fatal("RUNNER_SECRET / -secret is required")
	}
	drv = runner.NewDockerDriver()
	drv.Runtime = *runtime

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("/v1/deploy", auth(handleDeploy))
	mux.HandleFunc("/v1/apps/", auth(handleApp)) // wake/sleep/stop/status/delete by path

	log.Printf("tunr-runner listening on %s (runtime=%s)", *listen, *runtime)
	log.Fatal(http.ListenAndServe(*listen, mux))
}

// ---------- auth ----------

func auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(*secret)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// ---------- deploy (build + run) ----------

func handleDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sse, err := newSSE(w)
	if err != nil {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSourceBytes+1<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		sse.fail("bad multipart: " + err.Error())
		return
	}
	var meta DeployMeta
	if err := json.Unmarshal([]byte(r.FormValue("meta")), &meta); err != nil || meta.AppID == "" {
		sse.fail("bad meta json (app_id required)")
		return
	}
	src, _, err := r.FormFile("source")
	if err != nil {
		sse.fail("missing source file")
		return
	}
	defer src.Close()

	if !buildMu.TryLock() {
		sse.event("queued", "waiting for build slot")
		buildMu.Lock()
	}
	defer buildMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), buildTimeout)
	defer cancel()

	dir := filepath.Join(*workdir, meta.DeploymentID)
	defer os.RemoveAll(dir)

	sse.event("extracting", "")
	if err := extractTarGz(src, dir); err != nil {
		sse.fail("extract: " + err.Error())
		return
	}

	imageRef := "tunr-app-" + meta.AppID + ":" + orDefault(meta.DeploymentID, "latest")
	sse.event("building", detectHint(dir))
	if err := build(ctx, dir, imageRef, meta.NoCache, sse); err != nil {
		sse.fail(err.Error())
		return
	}

	sse.event("releasing", imageRef)
	ep, err := drv.Deploy(ctx, runner.DeploySpec{
		App: runner.AppSpec{
			ID:           meta.AppID,
			Name:         meta.Name,
			InternalPort: meta.InternalPort,
			Env:          meta.Env,
			CPUs:         meta.CPUs,
			MemoryMB:     meta.MemoryMB,
			EdgeSecret:   meta.EdgeSecret,
		},
		ImageRef:     imageRef,
		DeploymentID: meta.DeploymentID,
	})
	if err != nil {
		sse.fail("run: " + err.Error())
		return
	}
	sse.send(map[string]string{"event": "ready", "endpoint": ep.URL})
}

func build(ctx context.Context, dir, imageRef string, noCache bool, sse *sseWriter) error {
	// Dockerfile wins; otherwise Nixpacks auto-detects the stack.
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil {
		args := []string{"build", "-t", imageRef}
		if noCache {
			args = append(args, "--no-cache")
		}
		return streamCmd(ctx, sse, "docker", append(args, dir)...)
	}
	args := []string{"build", dir, "--name", imageRef}
	if noCache {
		args = append(args, "--no-cache")
	}
	return streamCmd(ctx, sse, "nixpacks", args...)
}

// ---------- lifecycle (wake/sleep/stop/status/delete) ----------

func handleApp(w http.ResponseWriter, r *http.Request) {
	// path: /v1/apps/{id}/{action}   or   /v1/apps/{id}  (DELETE)
	rest := strings.TrimPrefix(r.URL.Path, "/v1/apps/")
	parts := strings.SplitN(rest, "/", 2)
	appID := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}
	if appID == "" {
		http.Error(w, "app id required", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	switch {
	case r.Method == http.MethodDelete && action == "":
		if err := drv.Destroy(ctx, appID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"deleted": appID})
	case r.Method == http.MethodPost && action == "wake":
		_ = drv.Wake(ctx, appID) // best-effort; relay probes for readiness
		writeJSON(w, map[string]string{"woken": appID})
	case r.Method == http.MethodPost && action == "sleep":
		if err := drv.Sleep(ctx, appID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"slept": appID})
	case r.Method == http.MethodPost && action == "stop":
		if err := drv.Stop(ctx, appID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"stopped": appID})
	case r.Method == http.MethodGet && action == "status":
		st, err := drv.Status(ctx, appID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"status": string(st)})
	default:
		http.Error(w, "unknown action", http.StatusNotFound)
	}
}

// ---------- helpers ----------

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func detectHint(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil {
		return "Dockerfile detected"
	}
	return "nixpacks auto-detect"
}

func streamCmd(ctx context.Context, sse *sseWriter, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		sse.log(sc.Text())
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

func extractTarGz(r io.Reader, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	gz, err := gzip.NewReader(io.LimitReader(r, maxSourceBytes))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(hdr.Name)
		if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue
		}
		path := filepath.Join(dst, name)
		if !strings.HasPrefix(path, filepath.Clean(dst)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal path in archive: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			total += hdr.Size
			if total > maxExtractBytes {
				return fmt.Errorf("archive exceeds %d bytes uncompressed", maxExtractBytes)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, io.LimitReader(tr, hdr.Size)); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
}

// ---------- SSE + JSON ----------

type sseWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func newSSE(w http.ResponseWriter) (*sseWriter, error) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("no flusher")
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	return &sseWriter{w: w, f: f}, nil
}

func (s *sseWriter) send(v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(s.w, "data: %s\n\n", b)
	s.f.Flush()
}
func (s *sseWriter) event(ev, detail string) {
	s.send(map[string]string{"event": ev, "detail": detail})
}
func (s *sseWriter) log(line string) { s.send(map[string]string{"event": "log", "line": line}) }
func (s *sseWriter) fail(msg string) {
	log.Println("deploy failed:", msg)
	s.send(map[string]string{"event": "failed", "error": msg})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
