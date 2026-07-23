// buildd — tunr's build agent.
//
// Runs next to a Docker daemon (own server in v0; a Fly builder machine if the
// Fly path is ever used). The control plane POSTs a source tarball; buildd
// builds an image with Nixpacks (or `docker build` if a Dockerfile exists) and
// streams progress back as Server-Sent Events.
//
//	POST /build   Authorization: Bearer <shared-secret>
//	              multipart/form-data: meta (JSON) + source (tar.gz)
//	              -> text/event-stream of {"event":..., "line"/"error":...}
//
// Own-server note: no registry push needed — the built image is already local
// to the daemon the runner uses. Pass -push only on remote-builder setups.
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
)

const (
	maxSourceBytes  = 50 << 20  // 50 MB compressed upload cap (mirrors CLI cap)
	maxExtractBytes = 500 << 20 // uncompressed safety cap
	buildTimeout    = 15 * time.Minute
)

// BuildMeta is the JSON side of the multipart upload.
type BuildMeta struct {
	DeploymentID string            `json:"deployment_id"`
	ImageRef     string            `json:"image_ref"` // e.g. tunr-a-x7k2:dep_9f2c
	NoCache      bool              `json:"no_cache"`
	BuildEnv     map[string]string `json:"build_env"` // NIXPACKS_* etc.
}

var (
	listen  = flag.String("listen", ":9090", "listen address")
	workdir = flag.String("workdir", "/work", "scratch dir for extracted sources")
	secret  = flag.String("secret", os.Getenv("BUILDD_SECRET"), "bearer auth shared secret")
	doPush  = flag.Bool("push", false, "docker push after build (remote-builder setups only)")

	buildMu sync.Mutex // v0: one build at a time
)

func main() {
	flag.Parse()
	if *secret == "" {
		log.Fatal("BUILDD_SECRET / -secret is required")
	}
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	http.HandleFunc("/build", handleBuild)
	log.Printf("buildd listening on %s (push=%v)", *listen, *doPush)
	log.Fatal(http.ListenAndServe(*listen, nil))
}

// ---------- HTTP ----------

func handleBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !authOK(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
	var meta BuildMeta
	if err := json.Unmarshal([]byte(r.FormValue("meta")), &meta); err != nil || meta.ImageRef == "" {
		sse.fail("bad meta json")
		return
	}
	src, _, err := r.FormFile("source")
	if err != nil {
		sse.fail("missing source file")
		return
	}
	defer src.Close()

	// v0 queue: single global build slot; tell the client if it's waiting.
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

	sse.event("building", detectHint(dir))
	if err := runBuild(ctx, dir, meta, sse); err != nil {
		sse.fail(err.Error())
		return
	}

	if *doPush {
		sse.event("pushing", meta.ImageRef)
		if err := streamCmd(ctx, sse, "docker", "push", meta.ImageRef); err != nil {
			sse.fail("push: " + err.Error())
			return
		}
	}
	sse.event("done", meta.ImageRef)
}

func authOK(r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(got), []byte(*secret)) == 1
}

// ---------- Build steps ----------

func detectHint(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil {
		return "Dockerfile detected"
	}
	return "nixpacks auto-detect"
}

func runBuild(ctx context.Context, dir string, meta BuildMeta, sse *sseWriter) error {
	// Rule: an explicit Dockerfile wins (power-user escape hatch); otherwise
	// Nixpacks detects the stack (Node/Python/Go/...) and builds via Docker.
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil {
		args := []string{"build", "-t", meta.ImageRef}
		if meta.NoCache {
			args = append(args, "--no-cache")
		}
		return streamCmd(ctx, sse, "docker", append(args, dir)...)
	}
	args := []string{"build", dir, "--name", meta.ImageRef}
	if meta.NoCache {
		args = append(args, "--no-cache")
	}
	for k, v := range meta.BuildEnv {
		args = append(args, "--env", k+"="+v)
	}
	return streamCmd(ctx, sse, "nixpacks", args...)
}

// streamCmd runs a command and forwards each output line as an SSE "log" event.
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

// ---------- Safe tar.gz extraction ----------

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
		// Path-traversal guard: resolved path must stay under dst.
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
		default:
			// Symlinks/devices are dropped on purpose — build inputs only.
		}
	}
}

// ---------- SSE plumbing ----------

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
	log.Println("build failed:", msg)
	s.send(map[string]string{"event": "failed", "error": msg})
}
