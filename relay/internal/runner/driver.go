// Package runner abstracts "where tunr apps run" behind a small Driver
// interface, so the control plane never talks to Docker (or Fly) directly.
//
// v0 ships DockerDriver (own server, docker CLI + gVisor runtime).
// FlyDriver is a stub kept as an escape hatch / future region expansion.
//
// Design rule: the Driver knows nothing about routes, subdomains or HTTP —
// it only manages compute units and returns an Endpoint the relay can dial.
package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ahmetvural79/tunr/relay/internal/logger"
)

// logQoS reports a density lever that didn't apply. These are always advisory:
// the app runs correctly either way, we just don't get the memory back. Kept as
// one helper so the "this is not an error path" intent stays visible.
func logQoS(format string, args ...any) { logger.Warn("qos: "+format, args...) }

// ---------- Types ----------

// AppSpec describes a persistent app (stable across deployments).
type AppSpec struct {
	ID           string            // "a_x7k2..." — stable, used in container names
	Name         string            // subdomain, informational only here
	InternalPort int               // the port the app listens on inside (PORT env)
	Env          map[string]string // user env; PORT/TUNR_* are added by the driver
	CPUs         float64           // e.g. 1.0
	MemoryMB     int               // e.g. 256
	EdgeSecret   string            // HMAC key shared with relay (tunr-shim verifies)
}

// DeploySpec is a single (re)deploy of an app with a concrete image.
type DeploySpec struct {
	App          AppSpec
	ImageRef     string // local image tag (own server) or registry ref (fly)
	DeploymentID string // "dep_..." — used for labels/telemetry
}

type Status string

const (
	StatusRunning Status = "running"
	// StatusSleeping is the WARM state: the container is paused (cgroup freezer)
	// AND its pages have been reclaimed into zram, so it holds ~15-25 MB of real
	// RAM instead of a full ~70 MB resident set. Resume is a decompress, not a
	// disk read — still effectively instant. See DockerDriver.Sleep.
	StatusSleeping Status = "sleeping"
	StatusStopped  Status = "stopped" // cold — start takes seconds
	StatusUnknown  Status = "unknown"
)

// Endpoint is what the relay's CloudUpstream dials.
type Endpoint struct {
	URL string // e.g. http://172.20.0.7:8080 (docker) or http://tunr-a-x.flycast:80 (fly)
}

// Driver is the only surface the control plane and relay depend on.
type Driver interface {
	EnsureApp(ctx context.Context, app AppSpec) error           // idempotent one-time setup
	Deploy(ctx context.Context, d DeploySpec) (Endpoint, error) // (re)start unit with image
	Wake(ctx context.Context, appID string) error               // sleeping/stopped -> running
	Sleep(ctx context.Context, appID string) error              // running -> sleeping
	Stop(ctx context.Context, appID string) error               // -> stopped (frees RAM)
	Destroy(ctx context.Context, appID string) error            // remove everything
	Status(ctx context.Context, appID string) (Status, error)
	// Logs streams the unit's output. tail <= 0 means "all". follow keeps the
	// stream open; the caller's context is the only thing that ends it, so a
	// following caller must never wrap ctx in a lifecycle-length deadline.
	Logs(ctx context.Context, appID string, tail int, follow bool) (io.ReadCloser, error)
}

// ---------- DockerDriver (own server, v0) ----------

// Assumptions:
//   - a dedicated bridge network exists:
//     docker network create --opt com.docker.network.bridge.enable_icc=false tunr-apps
//   - gVisor installed and registered as a runtime named "runsc" (fallback: "runc" behind a flag)
//   - relay runs on the same host (or same L2), so it can dial container IPs / names directly.
//
// Wake-on-request wiring: the relay's CloudUpstream calls Wake() when a dial
// fails; an idle sweeper (control plane goroutine) calls Sleep()/Stop() based
// on last-request timestamps that the relay records per app.
type DockerDriver struct {
	Runtime string // "runsc" (default) or "runc"
	Network string // "tunr-apps"

	// Cgroups drives the Faz 1 density levers (reclaim-on-pause, memory.high /
	// memory.min QoS, PSI telemetry). Nil-safe: when the hierarchy isn't
	// reachable the driver still runs apps, just without those optimisations.
	Cgroups *Cgroups

	// Tier is the QoS class applied to every app container after it starts.
	Tier QoSTier

	// ReclaimBytes is how much the kernel is asked to push out of a paused
	// container's cgroup. It is an upper bound, not a target: the kernel stops
	// when there's nothing cold left, so over-asking is free.
	ReclaimBytes uint64

	// CPUBaseline pins the CPU feature set advertised to gVisor sandboxes, so
	// snapshots stay restorable on any node meeting that baseline. Empty or
	// "off" disables it, which is the DEFAULT — an unsupported value makes
	// runsc fail to create the sandbox at all, breaking every deploy. Read the
	// note in cpubaseline.go before enabling it.
	CPUBaseline string
}

// NewDockerDriver returns a DockerDriver with secure defaults (gVisor + isolated
// bridge) and the standard QoS tier.
func NewDockerDriver() *DockerDriver {
	return &DockerDriver{
		Runtime:      "runsc",
		Network:      "tunr-apps",
		Tier:         DefaultTier,
		ReclaimBytes: 512 << 20,
		CPUBaseline:  DefaultCPUBaseline, // off by default — see cpubaseline.go
	}
}

func (d *DockerDriver) container(appID string) string { return "tunr-app-" + appID }

// docker is a tiny exec helper; swap for the Docker SDK later if needed.
func (d *DockerDriver) docker(ctx context.Context, args ...string) (string, error) {
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func (d *DockerDriver) EnsureApp(ctx context.Context, app AppSpec) error {
	// Network is created out-of-band at server setup; nothing per-app needed in v0.
	// TODO(Faz1): per-app iptables egress allowlist chains keyed by container IP.
	return nil
}

func (d *DockerDriver) Deploy(ctx context.Context, dep DeploySpec) (Endpoint, error) {
	name := d.container(dep.App.ID)

	// v0 update strategy: stop & replace (relay's freeze cache covers the gap).
	_, _ = d.docker(ctx, "rm", "-f", name) // ignore "no such container"

	port := dep.App.InternalPort
	if port == 0 {
		port = 8080
	}
	// Memory model (Faz 1, lever L3). The old `--memory 256m` was a hard cap and
	// nothing else: an app either fit in 256 MB or was OOM-killed, and capacity
	// planning had to reserve the full 256 MB per app even though the typical
	// app resides at ~46 MB.
	//
	// Now the cap is the tier's hard ceiling and the *soft* cap (memory.high) is
	// applied to the cgroup after start — the kernel throttles and reclaims into
	// zram in between instead of killing. --memory-swap raises the combined
	// memory+swap allowance so those reclaimed pages have somewhere to land.
	tier := d.tierFor(dep.App)
	cpus := dep.App.CPUs
	if cpus == 0 {
		cpus = 1.0
	}

	args := []string{
		"run", "-d",
		"--name", name,
		"--runtime", d.Runtime,
		"--network", d.Network,
		"--memory", fmt.Sprintf("%dm", tier.MemoryMaxMB),
		"--memory-swap", fmt.Sprintf("%dm", tier.MemoryMaxMB*3), // 2× MemoryMax of swap headroom
		"--cpus", fmt.Sprintf("%.2f", cpus),
		"--pids-limit", "256",
		// OOM victim ordering (Faz 0). If the box ever does run out of memory,
		// the kernel must take a tenant app, never the relay, Postgres or the
		// runner — losing one app is an incident, losing the control plane is
		// an outage for everyone. A positive adj makes apps the first choice.
		"--oom-score-adj", "500",
		"--security-opt", "no-new-privileges",
		"--read-only", "--tmpfs", "/tmp:size=64m",
		"--restart", "no", // lifecycle is ours, not dockerd's
		"--label", "tunr.app=" + dep.App.ID,
		"--label", "tunr.deployment=" + dep.DeploymentID,
		"-e", fmt.Sprintf("PORT=%d", port),
		"-e", "TUNR_APP=" + dep.App.Name,
		"-e", "TUNR_EDGE_SECRET=" + dep.App.EdgeSecret, // consumed by tunr-shim
	}
	// Pin the sandbox's CPU feature set so future snapshots stay portable
	// across nodes. No-op today (one node); irreversible if deferred.
	args = append(args, d.cpuBaselineArgs(ctx)...)

	for k, v := range dep.App.Env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, dep.ImageRef)

	if _, err := d.docker(ctx, args...); err != nil {
		return Endpoint{}, err
	}
	ip, err := d.containerIP(ctx, name)
	if err != nil {
		return Endpoint{}, err
	}

	// The cgroup only exists once the container is running, so the soft cap and
	// reclaim protection are written here rather than passed to `docker run`.
	// Advisory: a failure costs density, not correctness.
	if err := d.applyQoS(ctx, dep.App.ID, tier); err != nil {
		logQoS("applyQoS %s: %v", dep.App.ID, err)
	}

	// Reclaim disk: nixpacks images are large (~900MB); drop this app's older tags.
	go d.pruneOldImages(context.Background(), dep.App.ID, dep.ImageRef)
	return Endpoint{URL: fmt.Sprintf("http://%s:%d", ip, port)}, nil
}

// tierFor resolves the QoS class for an app. An explicit per-app MemoryMB (set
// by the control plane for a paid resource profile) overrides the driver
// default; the soft cap tracks it at ~50% so there is always a throttle band
// between "busy" and "killed".
func (d *DockerDriver) tierFor(app AppSpec) QoSTier {
	t := d.Tier
	if t.MemoryMaxMB == 0 {
		t = DefaultTier
	}
	if app.MemoryMB > 0 {
		t.MemoryMaxMB = app.MemoryMB
		t.MemoryHighMB = app.MemoryMB / 2
	}
	return t
}

// containerRef resolves an app's full container ID and host PID — both needed
// to locate its cgroup. PID is 0 for a stopped container, which is fine: the
// path resolver falls back to name-based lookup.
func (d *DockerDriver) containerRef(ctx context.Context, appID string) (id string, pid int, err error) {
	out, err := d.docker(ctx, "inspect", "-f", "{{.Id}} {{.State.Pid}}", d.container(appID))
	if err != nil {
		return "", 0, err
	}
	idStr, pidStr, ok := strings.Cut(strings.TrimSpace(out), " ")
	if !ok {
		return "", 0, fmt.Errorf("unexpected inspect output for %s: %q", appID, out)
	}
	p, _ := strconv.Atoi(strings.TrimSpace(pidStr))
	return idStr, p, nil
}

func (d *DockerDriver) applyQoS(ctx context.Context, appID string, t QoSTier) error {
	if d.Cgroups == nil {
		return nil
	}
	cid, pid, err := d.containerRef(ctx, appID)
	if err != nil {
		return err
	}
	return d.Cgroups.ApplyQoS(cid, pid, t)
}

// Stats samples an app's cgroup (memory breakdown + PSI). Returns an error when
// cgroups are unavailable — callers treat that as "telemetry off", not a fault.
func (d *DockerDriver) Stats(ctx context.Context, appID string) (CgroupStats, error) {
	if d.Cgroups == nil {
		return CgroupStats{}, fmt.Errorf("cgroup telemetry disabled")
	}
	cid, pid, err := d.containerRef(ctx, appID)
	if err != nil {
		return CgroupStats{}, err
	}
	return d.Cgroups.Stats(cid, pid)
}

// List returns the app IDs of every container this driver manages, derived from
// the tunr.app label. Used by the telemetry loop, which must not depend on the
// control plane's view of what exists.
func (d *DockerDriver) List(ctx context.Context) ([]string, error) {
	out, err := d.docker(ctx, "ps", "-a", "--filter", "label=tunr.app",
		"--format", "{{.Label \"tunr.app\"}}")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			ids = append(ids, s)
		}
	}
	return ids, nil
}

// pruneOldImages removes every image tag for an app except the one just deployed.
func (d *DockerDriver) pruneOldImages(ctx context.Context, appID, keep string) {
	out, err := d.docker(ctx, "images", "tunr-app-"+appID, "--format", "{{.Repository}}:{{.Tag}}")
	if err != nil {
		return
	}
	for _, ref := range strings.Split(out, "\n") {
		ref = strings.TrimSpace(ref)
		if ref == "" || ref == keep {
			continue
		}
		_, _ = d.docker(ctx, "rmi", "-f", ref)
	}
}

func (d *DockerDriver) containerIP(ctx context.Context, name string) (string, error) {
	// index (not .Networks.<name>) because network names contain dashes.
	format := fmt.Sprintf(`{{(index .NetworkSettings.Networks "%s").IPAddress}}`, d.Network)
	ip, err := d.docker(ctx, "inspect", "-f", format, name)
	if err != nil || ip == "" || ip == "<no value>" {
		return "", fmt.Errorf("container %s has no IP on %s (err=%v)", name, d.Network, err)
	}
	return ip, nil
}

// IP returns the app container's current IP on the app network (after Wake/Deploy).
// Used so callers can re-target after a cold stop→start reassigns the IP.
func (d *DockerDriver) IP(ctx context.Context, appID string) (string, error) {
	return d.containerIP(ctx, d.container(appID))
}

func (d *DockerDriver) Wake(ctx context.Context, appID string) error {
	name := d.container(appID)
	st, _ := d.Status(ctx, appID)
	// Every branch is idempotent: concurrent requests for the same app all call
	// Wake, so losing a race to another waker must read as success, not failure.
	// A spurious error here is expensive — the relay logs it and keeps probing
	// until the wake budget expires, turning a working app into a 503.
	switch st {
	case StatusSleeping:
		_, err := d.docker(ctx, "unpause", name) // cgroup freezer resume: ~instant
		if err != nil && strings.Contains(err.Error(), "is not paused") {
			return nil // someone else already resumed it
		}
		return err
	case StatusStopped:
		_, err := d.docker(ctx, "start", name) // cold start: seconds; relay tolerates via wakeTimeout
		if err != nil && strings.Contains(err.Error(), "already started") {
			return nil
		}
		return err
	case StatusRunning:
		return nil
	default:
		return fmt.Errorf("wake %s: unknown state", appID)
	}
}

// Sleep puts an app into the WARM state: push its cold pages into zram, then
// freeze it (Faz 1, lever L2).
//
// `docker pause` alone is a cgroup freezer — it stops the CPU and leaves the
// full resident set in RAM, which is why a sleeping app used to cost as much as
// a running one. The reclaim step is what makes sleep nearly free: an idle
// app's heap compresses ~4:1, taking a sleeping app from ~48 MB to ~16 MB of
// real RAM, and waking is a decompress rather than a disk read.
//
// ── Order matters: reclaim BEFORE pause ──
//
// The obvious sequence is pause-then-reclaim: a frozen container's pages are
// cold by definition, so it looks like the ideal moment. In practice writing
// memory.reclaim against a frozen cgroup that still has a large resident set
// BLOCKS — reclaim needs the cgroup's own tasks to make progress (writeback,
// unmapping), and they are exactly what the freezer has stopped. Measured on
// production: the write did not return within 60s, while the same reclaim on
// the running cgroup completed in 68ms.
//
// That hang is not merely slow. os.WriteFile does not observe context
// cancellation, so it outlives the handler's deadline and wedges the caller's
// goroutine — which for the sweeper means scale-to-zero stops for every app.
//
// So: reclaim first, while the app can still service it, then freeze whatever
// is left. The app is idle by definition at this point, so the few milliseconds
// between the two steps cost nothing.
func (d *DockerDriver) Sleep(ctx context.Context, appID string) error {
	// Only a running container can be frozen. Pausing a stopped one is not just
	// a no-op — it fails, which the sweeper reads as "sleep didn't work", so it
	// retries every tick forever against an app that is already using no RAM.
	switch st, _ := d.Status(ctx, appID); st {
	case StatusStopped:
		return nil // already deeper asleep than WARM
	case StatusSleeping:
		return nil // already WARM
	}

	// Reclaim first — best-effort, and never allowed to delay the freeze.
	if d.Cgroups != nil {
		if cid, pid, err := d.containerRef(ctx, appID); err != nil {
			logQoS("sleep %s: cgroup lookup failed: %v", appID, err)
		} else {
			d.reclaimBounded(cid, pid)
		}
	}

	if _, err := d.docker(ctx, "pause", d.container(appID)); err != nil {
		// "already paused" is the desired end state, not a failure. Treating it
		// as an error made the sweeper retry every tick forever and — because a
		// failed Sleep leaves the relay believing the app is awake — the next
		// request would be proxied into a container that is in fact frozen.
		if !strings.Contains(err.Error(), "is already paused") {
			return err
		}
	}
	return nil
}

// reclaimWatchdog bounds how long Sleep will wait for the kernel. The write
// itself cannot be cancelled, so on expiry we stop WAITING for it and let the
// orphaned goroutine finish on its own — the pause proceeds regardless.
const reclaimWatchdog = 5 * time.Second

// reclaimBounded asks the kernel to reclaim the app's pages, giving up on the
// wait (not the work) if it takes too long.
func (d *DockerDriver) reclaimBounded(cid string, pid int) {
	// Size the request to what the cgroup actually holds rather than a flat
	// figure: asking for far more than exists makes the kernel scan hard for
	// pages that aren't there, which is the expensive part.
	want := d.ReclaimBytes
	if st, err := d.Cgroups.Stats(cid, pid); err == nil && st.CurrentBytes > 0 {
		want = st.CurrentBytes + st.CurrentBytes/4 // +25% headroom
	}

	done := make(chan error, 1) // buffered: the goroutine must never block on send
	go func() { done <- d.Cgroups.Reclaim(cid, pid, want) }()

	select {
	case err := <-done:
		if err != nil {
			// EIO/EAGAIN just means "couldn't reclaim the full amount", which is
			// normal — the kernel still reclaimed what it could.
			logQoS("reclaim %s: %v (partial reclaim is still a win)", short(cid), err)
		}
	case <-time.After(reclaimWatchdog):
		logQoS("reclaim %s: still running after %s — pausing anyway", short(cid), reclaimWatchdog)
	}
}

// Stop cold-stops an app, freeing its RAM entirely.
//
// A frozen container MUST be thawed first. `docker stop` works by sending
// SIGTERM, and a paused container cannot be signalled — under runsc the attempt
// fails with "cannot signal container in state paused" and, worse, leaves a
// stale containerd task behind. The container then reports itself exited while
// runsc still believes it is paused, and every subsequent `docker start` fails
// with "could not delete stale containerd task object". The app is wedged
// permanently and only `docker rm -f` plus a redeploy recovers it.
//
// This is the whole reason the WARM→STOPPED transition was previously disabled
// ("scale-to-zero is pause-only for now"): the transition always runs on an
// already-paused container, so it hit this every single time.
func (d *DockerDriver) Stop(ctx context.Context, appID string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	name := d.container(appID)

	// Thaw first. Ignore the error: "not paused" is the common case and simply
	// means there was nothing to undo.
	if st, _ := d.Status(ctx, appID); st == StatusSleeping {
		if _, err := d.docker(ctx, "unpause", name); err != nil {
			logQoS("stop %s: unpause before stop failed: %v", appID, err)
		}
	}

	_, err := d.docker(ctx, "stop", "-t", "10", name)
	if err != nil && strings.Contains(err.Error(), "is not running") {
		return nil // already stopped — the desired end state
	}
	return err
}

func (d *DockerDriver) Destroy(ctx context.Context, appID string) error {
	// Drop the cached cgroup path first — after `rm -f` the container is gone
	// and we could no longer resolve the ID to forget.
	if d.Cgroups != nil {
		if cid, _, err := d.containerRef(ctx, appID); err == nil {
			d.Cgroups.Forget(cid)
		}
	}
	_, err := d.docker(ctx, "rm", "-f", d.container(appID))
	return err
}

func (d *DockerDriver) Status(ctx context.Context, appID string) (Status, error) {
	out, err := d.docker(ctx, "inspect", "-f", "{{.State.Status}} {{.State.Paused}}", d.container(appID))
	if err != nil {
		return StatusUnknown, err
	}
	switch {
	case strings.HasSuffix(out, "true"): // paused
		return StatusSleeping, nil
	case strings.HasPrefix(out, "running"):
		return StatusRunning, nil
	case strings.HasPrefix(out, "exited"), strings.HasPrefix(out, "created"):
		return StatusStopped, nil
	}
	return StatusUnknown, nil
}

func (d *DockerDriver) Logs(ctx context.Context, appID string, tail int, follow bool) (io.ReadCloser, error) {
	args := []string{"logs"}
	if tail > 0 {
		args = append(args, "--tail", strconv.Itoa(tail))
	} else {
		args = append(args, "--tail", "all")
	}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, d.container(appID))
	cmd := exec.CommandContext(ctx, "docker", args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// Close() must reap the child. A `docker logs -f` that is never waited on
	// leaves a zombie per request, and this endpoint is the one users hammer
	// while a deploy misbehaves — exactly when the box can least afford it.
	return &cmdReader{ReadCloser: pipe, cmd: cmd}, nil
}

// cmdReader ties a pipe's lifetime to its process: closing it kills and reaps.
type cmdReader struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (c *cmdReader) Close() error {
	err := c.ReadCloser.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
	return err
}

// Compile-time check that DockerDriver satisfies Driver.
var _ Driver = (*DockerDriver)(nil)

// ---------- FlyDriver (stub — future escape hatch) ----------

// FlyDriver would implement the same interface against api.machines.dev.
// Key differences vs DockerDriver, kept here so the knowledge isn't lost:
//   - Endpoint.URL is the app's .flycast address (private Fly Proxy), NOT
//     .internal — only flycast traffic triggers Fly's autostart.
//   - Wake() is a NO-OP: Fly Proxy wakes machines on request by itself.
//   - Sleep() is a NO-OP: services[].autostop="suspend" handles idling.
//   - Deploy() = machines create/update with registry image ref (push required).
type FlyDriver struct {
	OrgToken string
	Region   string
}

var errFlyNotImplemented = fmt.Errorf("FlyDriver: not implemented in v0 (own-server DockerDriver is the v0 path)")

func (f *FlyDriver) EnsureApp(ctx context.Context, app AppSpec) error { return errFlyNotImplemented }
func (f *FlyDriver) Deploy(ctx context.Context, d DeploySpec) (Endpoint, error) {
	return Endpoint{}, errFlyNotImplemented
}
func (f *FlyDriver) Wake(ctx context.Context, appID string) error    { return nil } // Fly Proxy does it
func (f *FlyDriver) Sleep(ctx context.Context, appID string) error   { return nil } // autostop handles it
func (f *FlyDriver) Stop(ctx context.Context, appID string) error    { return errFlyNotImplemented }
func (f *FlyDriver) Destroy(ctx context.Context, appID string) error { return errFlyNotImplemented }
func (f *FlyDriver) Status(ctx context.Context, appID string) (Status, error) {
	return StatusUnknown, errFlyNotImplemented
}
func (f *FlyDriver) Logs(ctx context.Context, appID string, tail int, follow bool) (io.ReadCloser, error) {
	return nil, errFlyNotImplemented
}

var _ Driver = (*FlyDriver)(nil)
