package runner

// cgroup.go — cgroup v2 access for app containers (Yoğunluk planı Faz 0 + Faz 1).
//
// Why this file exists: `docker run --memory` gives us a hard cap and nothing
// else. The density levers the plan calls for all live one level below Docker,
// in the unified cgroup v2 hierarchy:
//
//	memory.reclaim   → WARM state: push a paused app's (definitionally cold)
//	                   pages into zram. 46 MB RSS becomes ~15 MB of real RAM.
//	memory.high      → soft cap: kernel throttles + reclaims instead of OOM-killing.
//	memory.min       → reclaim protection, i.e. the paid/free QoS differentiator.
//	memory.pressure  → PSI: the "are we about to OOM" signal the sweeper needs.
//	cpu.pressure     → PSI: tells us whether wake latency is CPU-bound.
//
// Everything here degrades to a no-op with a reason when the hierarchy isn't
// reachable (cgroup v1 host, missing /sys/fs/cgroup bind mount, non-Linux dev
// box). The caller must never depend on these succeeding — they are density
// optimisations, not correctness requirements.
//
// Container path resolution is layered, cheapest first:
//  1. /proc/<pid>/cgroup — authoritative, needs a shared PID namespace.
//  2. the two standard Docker layouts (systemd / cgroupfs driver).
//  3. a depth-bounded walk of the hierarchy.
//
// Resolved paths are cached per container ID; a container's cgroup path is
// stable for its lifetime, and the cache is dropped when the ID disappears.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// cgroupWalkDepth bounds the fallback search so a pathological hierarchy can't
// turn a stats poll into a filesystem crawl.
const cgroupWalkDepth = 4

// CgroupStats is one sample of a container's resource state.
//
// Bytes fields are 0 when the kernel reports "max" (no limit) or the file is
// unreadable — callers should treat 0 as "unknown", not "zero usage".
type CgroupStats struct {
	Path string `json:"path"`

	// memory.current + the memory.stat breakdown that matters for density.
	CurrentBytes uint64 `json:"current_bytes"` // total charged (anon + file + kernel)
	AnonBytes    uint64 `json:"anon_bytes"`    // heap — what zram compresses
	FileBytes    uint64 `json:"file_bytes"`    // page cache — shared across same-base apps
	SwapBytes    uint64 `json:"swap_bytes"`    // in zram; the WARM-state payoff
	HighBytes    uint64 `json:"high_bytes"`    // memory.high (0 = max)
	MaxBytes     uint64 `json:"max_bytes"`     // memory.max (0 = max)

	// PSI — "some" avg10, percent of the last 10s stalled on this resource.
	MemPressure float64 `json:"mem_pressure"`
	CPUPressure float64 `json:"cpu_pressure"`
	IOPressure  float64 `json:"io_pressure"`

	// memory.events counters — OOM here means the app hit memory.max.
	OOMEvents     uint64 `json:"oom_events"`
	OOMKillEvents uint64 `json:"oom_kill_events"`
	HighEvents    uint64 `json:"high_events"` // times throttled at memory.high
}

// EffectiveBytes is the uncompressed RAM this app holds directly.
//
// This is simply memory.current: on cgroup v2 that counter ALREADY excludes
// pages that have been swapped out (those are accounted in memory.swap.current
// instead). Subtracting swap from it would double-count the saving and report a
// sleeping app as costing zero — which is the number a capacity model would
// then happily oversubscribe against, straight into an OOM.
//
// The app's true cost is EffectiveBytes plus the *compressed* footprint of its
// swapped pages; see RealBytes, which needs the host's measured zram ratio.
func (s CgroupStats) EffectiveBytes() uint64 { return s.CurrentBytes }

// RealBytes estimates what this app actually costs the host, counting its
// swapped pages at their compressed size.
//
// ratio is the host's measured zram compression ratio (orig/compressed, ~4:1
// for an idle heap). A ratio <= 1 means "unknown" and the swapped pages are
// counted uncompressed — deliberately pessimistic, because over-reporting
// capacity is the expensive direction to be wrong in.
func (s CgroupStats) RealBytes(ratio float64) uint64 {
	if ratio <= 1 {
		return s.CurrentBytes + s.SwapBytes
	}
	return s.CurrentBytes + uint64(float64(s.SwapBytes)/ratio)
}

// Cgroups resolves and reads cgroup v2 paths for app containers.
//
// The zero value is unusable; construct with NewCgroups. Safe for concurrent use.
type Cgroups struct {
	root     string // hierarchy root, e.g. /sys/fs/cgroup
	procRoot string // /proc, used for the pid→cgroup fast path

	mu    sync.RWMutex
	cache map[string]string // container ID → absolute cgroup path

	// unavailable is set once when the hierarchy can't be used at all, so we
	// log the reason a single time instead of on every sweep tick.
	availOnce sync.Once
	availErr  error

	// swapSeen latches once swap is observed; noSwapWarn keeps the "soft cap
	// skipped" explanation to a single line.
	swapSeen   atomic.Bool
	noSwapWarn sync.Once
}

// NewCgroups builds a resolver. root/procRoot default to /sys/fs/cgroup and
// /proc; override them when the runner sees the host hierarchy at a bind mount
// (TUNR_CGROUP_ROOT / TUNR_PROC_ROOT).
func NewCgroups(root, procRoot string) *Cgroups {
	if root == "" {
		root = "/sys/fs/cgroup"
	}
	if procRoot == "" {
		procRoot = "/proc"
	}
	return &Cgroups{root: root, procRoot: procRoot, cache: map[string]string{}}
}

// Available reports whether a usable cgroup v2 hierarchy is present. The result
// is computed once and reused; the error explains what's missing so the runner
// can log an actionable warning at startup rather than failing silently.
func (c *Cgroups) Available() error {
	c.availOnce.Do(func() {
		// cgroup.controllers exists only on the unified (v2) hierarchy — its
		// presence is the standard v1-vs-v2 discriminator.
		probe := filepath.Join(c.root, "cgroup.controllers")
		if _, err := os.Stat(probe); err != nil {
			c.availErr = fmt.Errorf("cgroup v2 not reachable at %s: %w "+
				"(mount the host hierarchy into the runner, or the host is still on cgroup v1)", c.root, err)
			return
		}
		if _, err := os.ReadFile(probe); err != nil {
			c.availErr = fmt.Errorf("cgroup v2 at %s unreadable: %w", c.root, err)
		}
	})
	return c.availErr
}

// Forget drops a cached path (call when a container is destroyed or replaced —
// a redeploy creates a new container with a new cgroup).
func (c *Cgroups) Forget(containerID string) {
	c.mu.Lock()
	delete(c.cache, containerID)
	c.mu.Unlock()
}

// Path resolves a container's cgroup directory. pid may be 0 if unknown, in
// which case the /proc fast path is skipped.
func (c *Cgroups) Path(containerID string, pid int) (string, error) {
	if err := c.Available(); err != nil {
		return "", err
	}
	if containerID == "" {
		return "", fmt.Errorf("empty container id")
	}

	c.mu.RLock()
	cached, ok := c.cache[containerID]
	c.mu.RUnlock()
	if ok {
		// Re-validate: a restarted container keeps its ID but gets a fresh
		// cgroup, so a stale hit would silently write to a dead directory.
		if _, err := os.Stat(cached); err == nil {
			return cached, nil
		}
		c.Forget(containerID)
	}

	path, err := c.resolve(containerID, pid)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.cache[containerID] = path
	c.mu.Unlock()
	return path, nil
}

func (c *Cgroups) resolve(containerID string, pid int) (string, error) {
	// 1. /proc/<pid>/cgroup — authoritative when the PID namespace is shared.
	if pid > 0 {
		if p, err := c.pathFromProc(pid); err == nil {
			return p, nil
		}
	}

	// 2. The layouts Docker actually produces.
	candidates := []string{
		filepath.Join(c.root, "system.slice", "docker-"+containerID+".scope"), // systemd driver
		filepath.Join(c.root, "docker", containerID),                          // cgroupfs driver
		filepath.Join(c.root, "system.slice", "docker-"+containerID+".scope", "container"),
	}
	for _, p := range candidates {
		if isDir(p) {
			return p, nil
		}
	}

	// 3. Bounded walk — covers custom --cgroup-parent values.
	if p := c.walkFor(containerID); p != "" {
		return p, nil
	}
	return "", fmt.Errorf("no cgroup found for container %s under %s", short(containerID), c.root)
}

// pathFromProc reads the unified-hierarchy line ("0::/system.slice/...") from
// /proc/<pid>/cgroup and joins it onto our view of the root.
func (c *Cgroups) pathFromProc(pid int) (string, error) {
	f, err := os.Open(filepath.Join(c.procRoot, strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		// cgroup v2 always reports hierarchy id 0 with an empty controller list.
		rel, ok := strings.CutPrefix(sc.Text(), "0::")
		if !ok {
			continue
		}
		p := filepath.Join(c.root, filepath.Clean("/"+rel))
		if isDir(p) {
			return p, nil
		}
	}
	return "", fmt.Errorf("no cgroup v2 entry for pid %d", pid)
}

// walkFor searches the hierarchy for a directory whose name embeds the
// container ID, stopping at cgroupWalkDepth to bound the cost.
func (c *Cgroups) walkFor(containerID string) string {
	rootDepth := strings.Count(filepath.Clean(c.root), string(os.PathSeparator))
	found := ""
	_ = filepath.WalkDir(c.root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || found != "" {
			return nil //nolint:nilerr // unreadable subtrees are skipped, not fatal
		}
		if strings.Count(path, string(os.PathSeparator))-rootDepth > cgroupWalkDepth {
			return filepath.SkipDir
		}
		if strings.Contains(d.Name(), containerID) {
			found = path
		}
		return nil
	})
	return found
}

// ---------- reads ----------

// Stats samples a container's cgroup. Individual unreadable files leave their
// fields zero rather than failing the whole sample — a partial reading is more
// useful than none, and the fields are advisory.
func (c *Cgroups) Stats(containerID string, pid int) (CgroupStats, error) {
	path, err := c.Path(containerID, pid)
	if err != nil {
		return CgroupStats{}, err
	}
	st := CgroupStats{Path: path}

	st.CurrentBytes = readUint(filepath.Join(path, "memory.current"))
	st.HighBytes = readLimit(filepath.Join(path, "memory.high"))
	st.MaxBytes = readLimit(filepath.Join(path, "memory.max"))
	st.SwapBytes = readUint(filepath.Join(path, "memory.swap.current"))

	if kv := readKV(filepath.Join(path, "memory.stat")); kv != nil {
		st.AnonBytes = kv["anon"]
		st.FileBytes = kv["file"]
	}
	if kv := readKV(filepath.Join(path, "memory.events")); kv != nil {
		st.OOMEvents = kv["oom"]
		st.OOMKillEvents = kv["oom_kill"]
		st.HighEvents = kv["high"]
	}

	st.MemPressure = readPressureSome10(filepath.Join(path, "memory.pressure"))
	st.CPUPressure = readPressureSome10(filepath.Join(path, "cpu.pressure"))
	st.IOPressure = readPressureSome10(filepath.Join(path, "io.pressure"))
	return st, nil
}

// ---------- writes (the density levers) ----------

// HasSwap reports whether the host has any swap configured.
//
// Cached after the first positive result: swap is configured at boot and the
// answer is stable, but a negative result is re-checked so the runner picks up
// zram being enabled without needing a restart.
func (c *Cgroups) HasSwap() bool {
	if c.swapSeen.Load() {
		return true
	}
	if c.HostStats().SwapTotalBytes > 0 {
		c.swapSeen.Store(true)
		return true
	}
	return false
}

// warnNoSwapOnce explains the skipped soft cap exactly once, so the operator
// learns the lever is inert without every deploy logging it.
func (c *Cgroups) warnNoSwapOnce() {
	c.noSwapWarn.Do(func() {
		logQoS("memory.high NOT applied: host has no swap. " +
			"A soft cap without swap can't contain a cgroup and escalates to a system-wide OOM, " +
			"so it is safer to leave it unset. Run scripts/host-density.sh to enable zram.")
	})
}

// Reclaim asks the kernel to push up to `bytes` of this cgroup's pages out to
// swap. Called right after a pause: a frozen container's pages are cold by
// definition, so this is the cheapest 3× density win available (plan §3.1).
//
// Requires swap (zram) to exist — without it the kernel has nowhere to put the
// pages and the write returns EAGAIN/EINVAL after doing nothing.
func (c *Cgroups) Reclaim(containerID string, pid int, bytes uint64) error {
	if !c.HasSwap() {
		return fmt.Errorf("no swap configured — nothing to reclaim into (run scripts/host-density.sh)")
	}
	path, err := c.Path(containerID, pid)
	if err != nil {
		return err
	}
	f := filepath.Join(path, "memory.reclaim")
	if _, err := os.Stat(f); err != nil {
		return fmt.Errorf("memory.reclaim unavailable (needs kernel >= 5.19): %w", err)
	}
	if err := os.WriteFile(f, []byte(strconv.FormatUint(bytes, 10)), 0o644); err != nil {
		return fmt.Errorf("memory.reclaim %s: %w", short(containerID), err)
	}
	return nil
}

// QoSTier is a resource class applied directly to the cgroup after `docker run`.
//
// Docker's own flags can't express this: it maps --memory to memory.max and has
// no stable mapping for memory.high or memory.min across versions, so we write
// the files ourselves (plan §1.3, footnote).
type QoSTier struct {
	Name string

	// MemoryHighMB is the soft cap. Crossing it makes the kernel reclaim
	// aggressively and throttle the allocator — it does NOT kill. Set it
	// meaningfully below MemoryMaxMB so apps get squeezed before they die.
	//
	// CRITICAL: only effective when swap exists. Without swap, memory.high
	// containment fails and pressure escalates to a system-wide OOM instead
	// (Chris Down, LKML) — which is why zram is the prerequisite lever.
	MemoryHighMB int

	// MemoryMaxMB is the hard cap: exceeding it OOM-kills the app.
	MemoryMaxMB int

	// MemoryMinMB is reclaim-protected memory — the kernel will not reclaim
	// below it. This is the free-vs-paid differentiator: paid apps keep a
	// resident floor, free apps are fully reclaimable.
	MemoryMinMB int

	// CPUWeight is the cgroup v2 relative CPU share (default 100, range 1–10000).
	CPUWeight int
}

// DefaultTier is the standard app class: 384 MB hard cap with a 192 MB soft cap,
// so the common ~46 MB app has room to spike without either wasting a 256 MB
// reservation or being OOM-killed at the first burst.
var DefaultTier = QoSTier{
	Name: "standard", MemoryHighMB: 192, MemoryMaxMB: 384, MemoryMinMB: 0, CPUWeight: 100,
}

// ApplyQoS writes a tier onto a container's cgroup. Each field is applied
// independently: a kernel without one knob shouldn't cost us the others.
//
// SAFETY: memory.high is deliberately skipped when the host has no swap.
// Without somewhere to page out to, the kernel cannot contain a cgroup that
// crosses its soft cap; instead of throttling the one app responsible, pressure
// escalates into a system-wide OOM that can take down anything on the box.
// A soft cap is therefore not a "partial" improvement over no soft cap — it is
// strictly worse, so we refuse rather than half-apply it. (Chris Down, LKML.)
func (c *Cgroups) ApplyQoS(containerID string, pid int, t QoSTier) error {
	path, err := c.Path(containerID, pid)
	if err != nil {
		return err
	}
	var errs []string
	set := func(file, val string) {
		if err := os.WriteFile(filepath.Join(path, file), []byte(val), 0o644); err != nil {
			errs = append(errs, file+": "+err.Error())
		}
	}
	if t.MemoryHighMB > 0 {
		if c.HasSwap() {
			set("memory.high", mib(t.MemoryHighMB))
		} else {
			c.warnNoSwapOnce()
		}
	}
	if t.MemoryMaxMB > 0 {
		set("memory.max", mib(t.MemoryMaxMB))
	}
	if t.MemoryMinMB > 0 {
		set("memory.min", mib(t.MemoryMinMB))
	}
	if t.CPUWeight > 0 {
		set("cpu.weight", strconv.Itoa(t.CPUWeight))
	}
	if len(errs) > 0 {
		return fmt.Errorf("applyQoS %s: %s", short(containerID), strings.Join(errs, "; "))
	}
	return nil
}

// ---------- host-level ----------

// HostStats is the whole-box view the sweeper's safety valve reads. If the box
// is stalling on memory, apps get cooled aggressively before the kernel starts
// OOM-killing things we care about.
type HostStats struct {
	MemPressure    float64 `json:"mem_pressure"`     // /proc/pressure/memory, some avg10
	CPUPressure    float64 `json:"cpu_pressure"`     // /proc/pressure/cpu, some avg10
	IOPressure     float64 `json:"io_pressure"`      // /proc/pressure/io, some avg10
	MemTotalBytes  uint64  `json:"mem_total_bytes"`  //
	MemAvailBytes  uint64  `json:"mem_avail_bytes"`  // the number that actually predicts OOM
	SwapTotalBytes uint64  `json:"swap_total_bytes"` // 0 here means Faz 1 is not deployed
	SwapFreeBytes  uint64  `json:"swap_free_bytes"`

	// zram accounting, straight from the device. ZramOrigBytes is what was
	// handed to it; ZramUsedBytes is the real RAM it occupies after compression.
	// Their quotient is the only honest source for the compression ratio —
	// the "~3:1" in the plan is an estimate, this is the measurement.
	ZramOrigBytes  uint64 `json:"zram_orig_bytes"`
	ZramComprBytes uint64 `json:"zram_compr_bytes"`
	ZramUsedBytes  uint64 `json:"zram_used_bytes"`
}

// ZramRatio is the measured compression ratio (orig/used), or 0 when nothing
// has been swapped yet and there is nothing to measure.
func (h HostStats) ZramRatio() float64 {
	if h.ZramUsedBytes == 0 || h.ZramOrigBytes == 0 {
		return 0
	}
	return float64(h.ZramOrigBytes) / float64(h.ZramUsedBytes)
}

// MemUtilization is the fraction of RAM in use (0..1), 0 when unknown.
func (h HostStats) MemUtilization() float64 {
	if h.MemTotalBytes == 0 {
		return 0
	}
	return 1 - float64(h.MemAvailBytes)/float64(h.MemTotalBytes)
}

// HostStats samples system-wide PSI and meminfo.
func (c *Cgroups) HostStats() HostStats {
	h := HostStats{
		MemPressure: readPressureSome10(filepath.Join(c.procRoot, "pressure", "memory")),
		CPUPressure: readPressureSome10(filepath.Join(c.procRoot, "pressure", "cpu")),
		IOPressure:  readPressureSome10(filepath.Join(c.procRoot, "pressure", "io")),
	}
	// /proc/meminfo values are in kB.
	if kv := readMeminfo(filepath.Join(c.procRoot, "meminfo")); kv != nil {
		h.MemTotalBytes = kv["MemTotal"] * 1024
		h.MemAvailBytes = kv["MemAvailable"] * 1024
		h.SwapTotalBytes = kv["SwapTotal"] * 1024
		h.SwapFreeBytes = kv["SwapFree"] * 1024
	}
	h.ZramOrigBytes, h.ZramComprBytes, h.ZramUsedBytes = readZramStats(c.sysRoot())
	return h
}

// sysRoot locates /sys. The cgroup root is /sys/fs/cgroup on every layout we
// support, so /sys is two levels up — which keeps the containerised runner
// working without another bind mount to configure.
func (c *Cgroups) sysRoot() string {
	if strings.HasSuffix(filepath.Clean(c.root), filepath.Join("fs", "cgroup")) {
		return filepath.Dir(filepath.Dir(filepath.Clean(c.root)))
	}
	return "/sys"
}

// readZramStats parses /sys/block/zram*/mm_stat, summing across devices:
//
//	orig_data_size compr_data_size mem_used_total mem_limit mem_used_max ...
//
// mem_used_total is the figure that matters — it is the real RAM the device
// occupies, including its own allocator overhead, not just the compressed bytes.
func readZramStats(sysRoot string) (orig, compr, used uint64) {
	matches, err := filepath.Glob(filepath.Join(sysRoot, "block", "zram*", "mm_stat"))
	if err != nil {
		return 0, 0, 0
	}
	for _, path := range matches {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		f := strings.Fields(string(b))
		if len(f) < 3 {
			continue
		}
		o, _ := strconv.ParseUint(f[0], 10, 64)
		c, _ := strconv.ParseUint(f[1], 10, 64)
		u, _ := strconv.ParseUint(f[2], 10, 64)
		orig += o
		compr += c
		used += u
	}
	return orig, compr, used
}

// ---------- parsing helpers ----------

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func mib(n int) string { return strconv.FormatInt(int64(n)*1024*1024, 10) }

func readUint(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return v
}

// readLimit is readUint for files where "max" means unlimited (reported as 0).
func readLimit(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(b))
	if s == "max" {
		return 0
	}
	v, _ := strconv.ParseUint(s, 10, 64)
	return v
}

// readKV parses "key value" lines (memory.stat, memory.events).
func readKV(path string) map[string]uint64 {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		if err != nil {
			continue
		}
		out[k] = n
	}
	return out
}

// readMeminfo parses "Key:   1234 kB" lines.
func readMeminfo(path string) map[string]uint64 {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		out[k] = n
	}
	return out
}

// readPressureSome10 extracts avg10 from the "some" line of a PSI file:
//
//	some avg10=0.00 avg60=0.00 avg300=0.00 total=0
//	full avg10=0.00 ...
//
// "some" (any task stalled) is the right signal for capacity decisions; "full"
// (every task stalled) only trips once the box is already in trouble.
func readPressureSome10(path string) float64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || fields[0] != "some" {
			continue
		}
		for _, fld := range fields[1:] {
			if v, ok := strings.CutPrefix(fld, "avg10="); ok {
				n, err := strconv.ParseFloat(v, 64)
				if err != nil {
					return 0
				}
				return n
			}
		}
	}
	return 0
}
