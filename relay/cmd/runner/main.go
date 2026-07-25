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
	"sort"
	"strconv"
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

	// cgroupRoot / procRoot let the runner see the HOST hierarchy. The runner is
	// a container, so density levers (memory.reclaim, memory.high, PSI) only work
	// if /sys/fs/cgroup and /proc are bind-mounted in. Both degrade to no-ops.
	cgroupRoot = flag.String("cgroup-root", envOr("TUNR_CGROUP_ROOT", "/sys/fs/cgroup"), "cgroup v2 hierarchy root")
	procRoot   = flag.String("proc-root", envOr("TUNR_PROC_ROOT", "/proc"), "proc filesystem root (host proc for PSI)")

	// buildSlots caps concurrent builds. The old `buildMu` serialised them, so
	// the tenth person to deploy waited for nine full builds; three slots plus
	// the SCHED_IDLE build slice keeps the queue short without letting builds
	// starve wake latency.
	buildSlots = flag.Int("build-slots", envIntOr("TUNR_BUILD_SLOTS", 3), "max concurrent builds")

	// role separates the two jobs this process does today. They have opposite
	// resource profiles — building is CPU/NVMe-bursty and touches no live
	// traffic; running is RAM-heavy and latency-sensitive — which is why the
	// multi-node plan splits BUILDER off first: it's the noisiest component and
	// the least critical, so separating it is the best return for the risk.
	//
	// Deliberately a flag rather than two binaries. Same image, one process per
	// role, so moving builds to their own machine is a compose change instead
	// of a refactor. The boundary is real and enforced from today: an agent-only
	// runner rejects /v1/deploy outright.
	role = flag.String("role", envOr("TUNR_RUNNER_ROLE", "all"), "all | agent | builder")

	// cpuBaseline pins the CPU feature set gVisor exposes to sandboxes, so
	// snapshots stay restorable on other nodes. "off" disables it.
	// WARNING: changing this after checkpoint/restore ships invalidates every
	// existing snapshot — see internal/runner/cpubaseline.go.
	cpuBaseline = flag.String("cpu-baseline", envOr("TUNR_CPU_BASELINE", runner.DefaultCPUBaseline),
		"CPU feature baseline for gVisor sandboxes (e.g. x86-64-v2, or 'off')")

	drv    *runner.DockerDriver
	slice  *runner.BuildSlice
	builds *buildGate
)

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func envIntOr(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return d
}

// ---------- build admission ----------

// buildGate is a counting semaphore that also records how long builds waited.
// The queue-wait p95 is the metric the multi-node plan uses to decide when a
// separate BUILDER node is worth adding, so it has to be observable from day one.
type buildGate struct {
	slots chan struct{}

	mu       sync.Mutex
	waits    []time.Duration // rolling window of recent waits
	admitted int64
}

func newBuildGate(n int) *buildGate {
	return &buildGate{slots: make(chan struct{}, n)}
}

// acquire blocks until a slot frees up, returning how long it waited and a
// release func. A cancelled context aborts the wait.
func (g *buildGate) acquire(ctx context.Context) (time.Duration, func(), error) {
	start := time.Now()
	select {
	case g.slots <- struct{}{}:
	case <-ctx.Done():
		return time.Since(start), func() {}, ctx.Err()
	}
	waited := time.Since(start)

	g.mu.Lock()
	g.admitted++
	g.waits = append(g.waits, waited)
	if len(g.waits) > 200 { // bounded window — this is a gauge, not an audit log
		g.waits = g.waits[len(g.waits)-200:]
	}
	g.mu.Unlock()

	var once sync.Once
	return waited, func() { once.Do(func() { <-g.slots }) }, nil
}

// stats reports queue depth and wait percentiles for /v1/stats.
func (g *buildGate) stats() map[string]any {
	g.mu.Lock()
	w := append([]time.Duration(nil), g.waits...)
	admitted := g.admitted
	g.mu.Unlock()

	sort.Slice(w, func(i, j int) bool { return w[i] < w[j] })
	pct := func(p float64) float64 {
		if len(w) == 0 {
			return 0
		}
		i := int(p * float64(len(w)-1))
		return w[i].Seconds()
	}
	return map[string]any{
		"slots":          cap(g.slots),
		"in_flight":      len(g.slots),
		"admitted_total": admitted,
		"queue_wait_p50": pct(0.50),
		"queue_wait_p95": pct(0.95),
	}
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
	cg := runner.NewCgroups(*cgroupRoot, *procRoot)
	drv = runner.NewDockerDriver()
	drv.Runtime = *runtime
	drv.Cgroups = cg
	drv.CPUBaseline = *cpuBaseline

	// Density levers are optional but their absence is silent and expensive, so
	// say so loudly at startup: an operator seeing "OFF" knows the box is running
	// at pre-Faz-1 capacity and what to fix.
	if err := cg.Available(); err != nil {
		log.Printf("[warn] cgroup levers OFF: %v", err)
		log.Printf("[warn]   → sleeping apps keep their full RSS; memory.high/min and PSI telemetry unavailable")
	} else {
		host := cg.HostStats()
		log.Printf("cgroup levers ON (root=%s)", *cgroupRoot)
		if host.SwapTotalBytes == 0 {
			log.Printf("[warn] no swap configured — reclaim-on-pause has nowhere to put pages.")
			log.Printf("[warn]   → run scripts/host-density.sh to set up zram (Faz 1 prerequisite)")
		} else {
			log.Printf("swap: %d MB total, %d MB free", host.SwapTotalBytes>>20, host.SwapFreeBytes>>20)
		}
	}

	switch *role {
	case "all", "agent", "builder":
	default:
		log.Fatalf("invalid -role %q (want all | agent | builder)", *role)
	}
	buildsEnabled := *role == "all" || *role == "builder"
	agentEnabled := *role == "all" || *role == "agent"

	if buildsEnabled {
		builds = newBuildGate(*buildSlots)
		slice = runner.NewBuildSlice(cg, runner.DefaultBuildSlice)
		if err := slice.Ensure(context.Background()); err != nil {
			log.Printf("[warn] %s", slice.Describe())
		} else {
			log.Printf("%s", slice.Describe())
		}
		go pruneLoop() // periodic build-cache reclamation
	} else {
		// Still constructed so /v1/stats and streamCmd have something valid to
		// talk to; zero slots is unreachable because /v1/deploy isn't mounted.
		builds = newBuildGate(1)
		slice = runner.NewBuildSlice(cg, runner.DefaultBuildSlice)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	if buildsEnabled {
		mux.HandleFunc("/v1/deploy", auth(handleDeploy))
	}
	if agentEnabled {
		mux.HandleFunc("/v1/apps/", auth(handleApp)) // wake/sleep/stop/status/delete by path
	}
	// /v1/host is the cheap sample the relay's sweeper polls every tick;
	// /v1/stats is the full per-app picture for dashboards and capacity review.
	mux.HandleFunc("/v1/host", auth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, cg.HostStats())
	}))
	mux.HandleFunc("/v1/stats", auth(handleStats(cg)))

	log.Printf("tunr-runner listening on %s (role=%s, runtime=%s, build-slots=%d, cpu-baseline=%s)",
		*listen, *role, *runtime, *buildSlots, drv.CPUBaseline)
	log.Fatal(http.ListenAndServe(*listen, mux))
}

// ---------- telemetry (Faz 0) ----------

// handleStats is the observability endpoint the whole density programme rests
// on: you cannot safely oversubscribe what you haven't measured. It reports the
// host's memory/PSI picture plus a per-app cgroup sample, which together answer
// "how many apps are hot, what do they really cost, and is the box stalling?".
func handleStats(cg *runner.Cgroups) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()

		host := cg.HostStats()
		ratio := host.ZramRatio()
		out := map[string]any{
			"host":       host,
			"zram_ratio": ratio,
			"builds":     builds.stats(),
		}

		apps := map[string]any{}
		ids, err := drv.List(ctx)
		if err != nil {
			out["apps_error"] = err.Error()
		}
		var residentTotal, realTotal, swapTotal uint64
		for _, id := range ids {
			st, err := drv.Stats(ctx, id)
			if err != nil {
				apps[id] = map[string]string{"error": err.Error()}
				continue
			}
			status, _ := drv.Status(ctx, id)
			apps[id] = map[string]any{
				"status": string(status),
				// resident_bytes is memory.current: RAM held directly. It
				// already excludes swapped-out pages.
				"resident_bytes": st.CurrentBytes,
				"anon_bytes":     st.AnonBytes,
				"file_bytes":     st.FileBytes,
				// swap_bytes is the UNCOMPRESSED size of what moved to zram.
				"swap_bytes": st.SwapBytes,
				// real_bytes is what this app actually costs the host:
				// resident + its swapped pages at their compressed size.
				"real_bytes":      st.RealBytes(ratio),
				"mem_pressure":    st.MemPressure,
				"cpu_pressure":    st.CPUPressure,
				"oom_kill_events": st.OOMKillEvents,
				"high_events":     st.HighEvents,
			}
			residentTotal += st.CurrentBytes
			swapTotal += st.SwapBytes
			realTotal += st.RealBytes(ratio)
		}
		out["apps"] = apps
		// The headline density numbers. "uncompressed" is what the app
		// population would cost without zram; "real" is what it costs with it.
		uncompressed := residentTotal + swapTotal
		out["totals"] = map[string]any{
			"app_count":              len(ids),
			"resident_bytes":         residentTotal,
			"swapped_bytes":          swapTotal,
			"uncompressed_bytes":     uncompressed,
			"real_bytes":             realTotal,
			"reclaim_saving_percent": pctSaved(uncompressed, realTotal),
		}
		writeJSON(w, out)
	}
}

// pctSaved reports how much of the would-be footprint zram gave back.
func pctSaved(uncompressed, real uint64) float64 {
	if uncompressed == 0 {
		return 0
	}
	return (1 - float64(real)/float64(uncompressed)) * 100
}

// pruneLoop periodically reclaims Docker build cache + dangling images. Nixpacks
// builds accumulate cache fast; without this the disk fills up (a real constraint).
func pruneLoop() {
	tick := time.NewTicker(6 * time.Hour)
	defer tick.Stop()
	for range tick.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		_ = exec.CommandContext(ctx, "docker", "builder", "prune", "-f", "--keep-storage", "20GB").Run()
		_ = exec.CommandContext(ctx, "docker", "image", "prune", "-f").Run()
		cancel()
		log.Println("prune: reclaimed build cache (kept 20GB) + dangling images")
	}
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

	// Admission: up to build-slots builds run at once. Anything beyond that
	// queues, and the wait is both reported to the user (so a slow deploy is
	// explained rather than mysterious) and recorded for the queue-wait metric.
	gateCtx, gateCancel := context.WithTimeout(r.Context(), buildTimeout)
	waited, release, err := builds.acquire(gateCtx)
	gateCancel()
	if err != nil {
		sse.fail("build queue wait cancelled: " + err.Error())
		return
	}
	defer release()
	if waited > time.Second {
		sse.event("queued", fmt.Sprintf("waited %.0fs for a build slot", waited.Seconds()))
	}

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
	// 1. explicit Dockerfile wins (power-user escape hatch).
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil {
		return dockerBuild(ctx, dir, imageRef, noCache, sse)
	}
	// 2. our slim/distroless Dockerfile for common stacks → small images (~150MB).
	if label := generateSlimDockerfile(dir); label != "" {
		sse.event("building", "slim image ("+label+")")
		return dockerBuild(ctx, dir, imageRef, noCache, sse)
	}
	// 3. Nixpacks fallback — any stack, but a large (~900MB) image.
	sse.event("building", "nixpacks (fallback)")
	args := []string{"build", dir, "--name", imageRef}
	if noCache {
		args = append(args, "--no-cache")
	}
	// nixpacks shells out to `docker build` itself, so --cgroup-parent can't be
	// threaded through. Adopting the nixpacks process into the build slice at
	// least deprioritises its own (substantial) plan-generation work.
	return streamCmd(ctx, sse, "nixpacks", args...)
}

func dockerBuild(ctx context.Context, dir, imageRef string, noCache bool, sse *sseWriter) error {
	args := []string{"build", "-t", imageRef}
	if noCache {
		args = append(args, "--no-cache")
	}
	// Put the build's RUN steps in the SCHED_IDLE slice so compiling never
	// outbids an app trying to wake.
	args = append(args, slice.DockerArgs()...)
	return streamCmd(ctx, sse, "docker", append(args, dir)...)
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
		ip, _ := drv.IP(ctx, appID)
		writeJSON(w, map[string]string{"woken": appID, "ip": ip})
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
	// Deprioritise the build helper itself. Only reaches work this process does
	// locally — Docker daemon work is covered by --cgroup-parent instead.
	if slice.Active() && cmd.Process != nil {
		if err := slice.Adopt(cmd.Process.Pid); err != nil {
			log.Printf("[warn] build slice adopt %s: %v", name, err)
		}
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
