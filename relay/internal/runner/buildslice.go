package runner

// buildslice.go — CPU/IO/memory isolation for builds (Yoğunluk planı Faz 1, lever L4).
//
// The problem: builds are the noisiest neighbour on the box. nixpacks or buildx
// will happily saturate all four cores, and while that happens every app that
// tries to wake competes with the compiler for CPU — so wake latency, the one
// number users actually feel, spikes exactly when someone is deploying.
//
// The fix is not to make builds slower; it's to make them *yielding*. A cgroup
// with cpu.idle=1 (SCHED_IDLE) runs only on cores nothing else wants. A build
// alone on the box still gets the whole machine; the moment an app needs to
// wake, the scheduler hands the core over. io.weight and memory.max stop the
// same thing happening on the disk and memory axes.
//
// Best-effort by design: if the slice can't be created the build still runs,
// just without priority separation. That's the pre-Faz-1 behaviour, not a fault.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// BuildSliceConfig tunes the build cgroup. Defaults come from the plan: a
// tenth of serving's CPU weight, SCHED_IDLE, low IO share, 3 GB ceiling.
type BuildSliceConfig struct {
	// Name is the cgroup directory name. It must end in ".slice" when Docker
	// uses the systemd cgroup driver, because that's the only form systemd
	// accepts for --cgroup-parent.
	Name string

	CPUWeight int    // cgroup v2 cpu.weight; serving stays at the default 100
	CPUIdle   bool   // cpu.idle=1 → SCHED_IDLE: only runs on otherwise-free cores
	IOWeight  int    // io.weight
	MemoryMax string // memory.max, e.g. "3G"
}

// DefaultBuildSlice is the configuration from the plan (§1.4).
var DefaultBuildSlice = BuildSliceConfig{
	Name:      "tunr-build.slice",
	CPUWeight: 10,
	CPUIdle:   true,
	IOWeight:  10,
	MemoryMax: "3G",
}

// BuildSlice is a prepared build cgroup plus the knowledge of how to hand it to
// Docker. The zero value is a valid no-op.
type BuildSlice struct {
	cfg  BuildSliceConfig
	cg   *Cgroups
	path string // absolute cgroup dir, empty when unavailable

	// cgroupParent is the value to pass to `docker build --cgroup-parent`, in
	// whichever form the daemon's cgroup driver expects. Empty = don't pass it.
	cgroupParent string

	once sync.Once
	err  error
}

// NewBuildSlice returns an unprepared slice; call Ensure before use.
func NewBuildSlice(cg *Cgroups, cfg BuildSliceConfig) *BuildSlice {
	if cfg.Name == "" {
		cfg = DefaultBuildSlice
	}
	return &BuildSlice{cfg: cfg, cg: cg}
}

// Ensure creates and configures the cgroup once. The returned error explains
// why isolation is unavailable; it is informational, not fatal.
func (b *BuildSlice) Ensure(ctx context.Context) error {
	b.once.Do(func() { b.err = b.prepare(ctx) })
	return b.err
}

func (b *BuildSlice) prepare(ctx context.Context) error {
	if b.cg == nil {
		return fmt.Errorf("cgroups disabled")
	}
	if err := b.cg.Available(); err != nil {
		return err
	}

	driver := dockerCgroupDriver(ctx)
	name := b.cfg.Name
	// The cgroupfs driver takes a path and has no .slice convention; systemd
	// requires the .slice suffix and places it at the hierarchy root.
	if driver == "cgroupfs" {
		name = strings.TrimSuffix(name, ".slice")
	}
	path := filepath.Join(b.cg.root, name)

	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}

	// Enable the controllers we're about to configure. Without this the knob
	// files don't exist in the new directory.
	_ = os.WriteFile(filepath.Join(b.cg.root, "cgroup.subtree_control"),
		[]byte("+cpu +io +memory"), 0o644)

	var applied []string
	set := func(file, val string) {
		if err := os.WriteFile(filepath.Join(path, file), []byte(val), 0o644); err == nil {
			applied = append(applied, file+"="+val)
		}
	}
	if b.cfg.CPUWeight > 0 {
		set("cpu.weight", strconv.Itoa(b.cfg.CPUWeight))
	}
	if b.cfg.CPUIdle {
		// cpu.idle must be written after cpu.weight: setting idle resets weight
		// handling, and the kernel rejects a weight write on an idle group.
		set("cpu.idle", "1")
	}
	if b.cfg.IOWeight > 0 {
		set("io.weight", strconv.Itoa(b.cfg.IOWeight))
	}
	if b.cfg.MemoryMax != "" {
		set("memory.max", b.cfg.MemoryMax)
	}
	if len(applied) == 0 {
		return fmt.Errorf("no cgroup knobs writable under %s (read-only mount?)", path)
	}

	b.path = path
	if driver == "cgroupfs" {
		b.cgroupParent = "/" + name
	} else {
		b.cgroupParent = name
	}
	return nil
}

// DockerArgs returns the flags to splice into a `docker build` invocation so the
// build's RUN steps land in the slice. Empty when isolation is unavailable.
func (b *BuildSlice) DockerArgs() []string {
	if b.cgroupParent == "" {
		return nil
	}
	return []string{"--cgroup-parent", b.cgroupParent}
}

// Adopt moves a process into the slice. Use it for build helpers we spawn
// directly (nixpacks does real local work before it ever calls Docker), where
// --cgroup-parent doesn't reach.
func (b *BuildSlice) Adopt(pid int) error {
	if b.path == "" {
		return fmt.Errorf("build slice unavailable")
	}
	return os.WriteFile(filepath.Join(b.path, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0o644)
}

// Active reports whether isolation is actually in effect.
func (b *BuildSlice) Active() bool { return b.path != "" }

// Describe renders the slice state for a startup log line.
func (b *BuildSlice) Describe() string {
	if b.path == "" {
		return "build isolation: OFF (" + errText(b.err) + ")"
	}
	return fmt.Sprintf("build isolation: ON (%s, cpu.weight=%d cpu.idle=%v io.weight=%d memory.max=%s)",
		b.path, b.cfg.CPUWeight, b.cfg.CPUIdle, b.cfg.IOWeight, b.cfg.MemoryMax)
}

func errText(err error) string {
	if err == nil {
		return "not prepared"
	}
	return err.Error()
}

// dockerCgroupDriver asks the daemon which cgroup driver it uses; that decides
// the shape of --cgroup-parent. Defaults to systemd, which is what Docker on
// modern Debian/Ubuntu uses.
func dockerCgroupDriver(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "docker", "info", "-f", "{{.CgroupDriver}}").Output()
	if err != nil {
		return "systemd"
	}
	if d := strings.TrimSpace(string(out)); d != "" {
		return d
	}
	return "systemd"
}
