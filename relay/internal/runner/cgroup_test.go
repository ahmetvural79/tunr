package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeHierarchy builds a minimal cgroup v2 tree on disk so the parsing and
// path-resolution logic can be exercised without root or a real kernel.
func fakeHierarchy(t *testing.T) (root string, cid string) {
	t.Helper()
	root = t.TempDir()
	cid = "abc123def456abc123def456abc123def456abc123def456abc123def4560000"

	if err := os.WriteFile(filepath.Join(root, "cgroup.controllers"), []byte("cpu io memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "system.slice", "docker-"+cid+".scope")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("memory.current", "73400320\n")      // 70 MiB
	write("memory.high", "201326592\n")        // 192 MiB
	write("memory.max", "402653184\n")         // 384 MiB
	write("memory.swap.current", "52428800\n") // 50 MiB pushed into zram
	write("memory.stat", "anon 41943040\nfile 20971520\nkernel_stack 65536\nslab 1048576\n")
	write("memory.events", "low 0\nhigh 3\nmax 0\noom 0\noom_kill 0\n")
	write("memory.pressure", "some avg10=1.25 avg60=0.80 avg300=0.10 total=1234\nfull avg10=0.50 avg60=0.20 avg300=0.05 total=567\n")
	write("cpu.pressure", "some avg10=12.50 avg60=4.00 avg300=1.00 total=9999\n")
	write("io.pressure", "some avg10=0.00 avg60=0.00 avg300=0.00 total=0\n")
	return root, cid
}

// fakeProc builds a procfs stub reporting the given swap total in bytes.
// Levers that require swap consult this, so tests exercising them must supply
// a proc root rather than the host's (which has no swap on a dev machine).
func fakeProc(t *testing.T, swapBytes uint64) string {
	t.Helper()
	proc := t.TempDir()
	meminfo := fmt.Sprintf("MemTotal:       16384000 kB\nMemAvailable:    8192000 kB\n"+
		"SwapTotal:      %d kB\nSwapFree:       %d kB\n", swapBytes/1024, swapBytes/1024)
	if err := os.WriteFile(filepath.Join(proc, "meminfo"), []byte(meminfo), 0o644); err != nil {
		t.Fatal(err)
	}
	return proc
}

func TestCgroupsAvailable(t *testing.T) {
	root, _ := fakeHierarchy(t)
	if err := NewCgroups(root, "/proc").Available(); err != nil {
		t.Fatalf("expected available, got %v", err)
	}

	// A directory without cgroup.controllers is cgroup v1 (or not a hierarchy
	// at all) — the levers must report unavailable rather than half-work.
	if err := NewCgroups(t.TempDir(), "/proc").Available(); err == nil {
		t.Fatal("expected unavailable for a non-v2 root")
	}
}

func TestCgroupsPathResolvesSystemdLayout(t *testing.T) {
	root, cid := fakeHierarchy(t)
	cg := NewCgroups(root, "/proc")

	path, err := cg.Path(cid, 0)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join(root, "system.slice", "docker-"+cid+".scope")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestCgroupsPathResolvesCgroupfsLayout(t *testing.T) {
	root := t.TempDir()
	cid := "ffffffffffff0000"
	if err := os.WriteFile(filepath.Join(root, "cgroup.controllers"), []byte("memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docker", cid), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := NewCgroups(root, "/proc").Path(cid, 0)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if got != filepath.Join(root, "docker", cid) {
		t.Fatalf("path = %q", got)
	}
}

// A container that restarted keeps its ID but gets a fresh cgroup. A stale
// cached path would silently send reclaim and QoS writes into a dead directory,
// so the cache must re-validate.
func TestCgroupsPathInvalidatesStaleCache(t *testing.T) {
	root, cid := fakeHierarchy(t)
	cg := NewCgroups(root, "/proc")

	if _, err := cg.Path(cid, 0); err != nil {
		t.Fatalf("first Path: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, "system.slice")); err != nil {
		t.Fatal(err)
	}
	if _, err := cg.Path(cid, 0); err == nil {
		t.Fatal("expected error after the cgroup disappeared, got a cached hit")
	}
}

func TestCgroupsStats(t *testing.T) {
	root, cid := fakeHierarchy(t)

	st, err := NewCgroups(root, "/proc").Stats(cid, 0)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.CurrentBytes != 73400320 {
		t.Errorf("CurrentBytes = %d", st.CurrentBytes)
	}
	if st.AnonBytes != 41943040 {
		t.Errorf("AnonBytes = %d", st.AnonBytes)
	}
	if st.FileBytes != 20971520 {
		t.Errorf("FileBytes = %d", st.FileBytes)
	}
	if st.SwapBytes != 52428800 {
		t.Errorf("SwapBytes = %d", st.SwapBytes)
	}
	if st.HighBytes != 201326592 {
		t.Errorf("HighBytes = %d", st.HighBytes)
	}
	if st.HighEvents != 3 {
		t.Errorf("HighEvents = %d", st.HighEvents)
	}
	// PSI must come from the "some" line, not "full".
	if st.MemPressure != 1.25 {
		t.Errorf("MemPressure = %v, want 1.25", st.MemPressure)
	}
	if st.CPUPressure != 12.50 {
		t.Errorf("CPUPressure = %v, want 12.5", st.CPUPressure)
	}
}

// memory.current already EXCLUDES swapped-out pages on cgroup v2, so the
// resident figure must be reported as-is. Subtracting swap from it double-counts
// the saving and reports a sleeping app as free — the exact error that would let
// a capacity model oversubscribe straight into an OOM.
func TestEffectiveBytesIsResidentOnly(t *testing.T) {
	s := CgroupStats{CurrentBytes: 4 << 20, SwapBytes: 43 << 20}
	if got := s.EffectiveBytes(); got != 4<<20 {
		t.Fatalf("EffectiveBytes = %d, want the 4MiB resident figure", got)
	}
}

// RealBytes is what the app actually costs: resident plus its swapped pages at
// their compressed size.
func TestRealBytes(t *testing.T) {
	// 4 MiB resident + 40 MiB swapped at 4:1 => 4 + 10 = 14 MiB.
	s := CgroupStats{CurrentBytes: 4 << 20, SwapBytes: 40 << 20}
	if got := s.RealBytes(4.0); got != 14<<20 {
		t.Fatalf("RealBytes = %d, want 14MiB", got)
	}

	// An unknown ratio must count swap uncompressed. Over-reporting capacity is
	// the expensive direction to be wrong in, so this errs pessimistic.
	if got := s.RealBytes(0); got != 44<<20 {
		t.Fatalf("RealBytes(unknown) = %d, want 44MiB (uncompressed)", got)
	}
	if got := s.RealBytes(1.0); got != 44<<20 {
		t.Fatalf("RealBytes(1.0) = %d, want 44MiB", got)
	}

	// A HOT app with nothing swapped costs exactly its resident set.
	hot := CgroupStats{CurrentBytes: 47 << 20}
	if got := hot.RealBytes(4.0); got != 47<<20 {
		t.Fatalf("RealBytes(hot) = %d, want 47MiB", got)
	}
}

func TestZramRatio(t *testing.T) {
	h := HostStats{ZramOrigBytes: 43 << 20, ZramUsedBytes: 11 << 20}
	if got := h.ZramRatio(); got < 3.8 || got > 4.0 {
		t.Fatalf("ZramRatio = %v, want ~3.9", got)
	}
	// Nothing swapped yet: no measurement, and callers must treat 0 as unknown
	// rather than as "infinite compression".
	if got := (HostStats{}).ZramRatio(); got != 0 {
		t.Fatalf("ZramRatio with no data = %v, want 0", got)
	}
}

func TestReadZramStats(t *testing.T) {
	sys := t.TempDir()
	dir := filepath.Join(sys, "block", "zram0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// orig compr mem_used_total mem_limit mem_used_max same_pages compacted huge
	if err := os.WriteFile(filepath.Join(dir, "mm_stat"),
		[]byte("45400064 10590208 12058624 0 12058624 0 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	orig, compr, used := readZramStats(sys)
	if orig != 45400064 || compr != 10590208 || used != 12058624 {
		t.Fatalf("readZramStats = %d, %d, %d", orig, compr, used)
	}

	// No zram device at all is the pre-Faz-1 state, not an error.
	o, c, u := readZramStats(t.TempDir())
	if o != 0 || c != 0 || u != 0 {
		t.Fatalf("expected zeros with no zram device, got %d %d %d", o, c, u)
	}
}

func TestReadLimitTreatsMaxAsUnlimited(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "memory.max")
	if err := os.WriteFile(p, []byte("max\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readLimit(p); got != 0 {
		t.Fatalf("readLimit(max) = %d, want 0", got)
	}
	if err := os.WriteFile(p, []byte("12345\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readLimit(p); got != 12345 {
		t.Fatalf("readLimit = %d", got)
	}
}

func TestReadPressureSome10(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "pressure")
	// "full" comes first here to prove the parser keys on the label, not order.
	content := "full avg10=99.00 avg60=1.00 avg300=1.00 total=1\n" +
		"some avg10=3.75 avg60=1.00 avg300=1.00 total=1\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readPressureSome10(p); got != 3.75 {
		t.Fatalf("got %v, want 3.75", got)
	}
	// A missing PSI file (kernel without CONFIG_PSI) reads as zero pressure
	// rather than failing the sample.
	if got := readPressureSome10(filepath.Join(dir, "nope")); got != 0 {
		t.Fatalf("missing file = %v, want 0", got)
	}
}

func TestApplyQoSWritesKnobs(t *testing.T) {
	root, cid := fakeHierarchy(t)
	cg := NewCgroups(root, fakeProc(t, 10<<30)) // swap present: soft cap is safe to apply

	tier := QoSTier{MemoryHighMB: 192, MemoryMaxMB: 384, MemoryMinMB: 32, CPUWeight: 100}
	if err := cg.ApplyQoS(cid, 0, tier); err != nil {
		t.Fatalf("ApplyQoS: %v", err)
	}

	dir := filepath.Join(root, "system.slice", "docker-"+cid+".scope")
	for file, want := range map[string]string{
		"memory.high": "201326592",
		"memory.max":  "402653184",
		"memory.min":  "33554432",
		"cpu.weight":  "100",
	} {
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if string(b) != want {
			t.Errorf("%s = %q, want %q", file, b, want)
		}
	}
}

func TestReclaimRequiresKernelSupport(t *testing.T) {
	root, cid := fakeHierarchy(t)
	cg := NewCgroups(root, fakeProc(t, 10<<30)) // isolate the kernel-support check from the swap check

	// No memory.reclaim file → kernel < 5.19. Must be a clear error, not a
	// silent no-op, so the operator learns the lever isn't working.
	if err := cg.Reclaim(cid, 0, 512<<20); err == nil {
		t.Fatal("expected an error when memory.reclaim is absent")
	}

	dir := filepath.Join(root, "system.slice", "docker-"+cid+".scope")
	if err := os.WriteFile(filepath.Join(dir, "memory.reclaim"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cg.Reclaim(cid, 0, 512<<20); err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "memory.reclaim"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "536870912" {
		t.Fatalf("memory.reclaim = %q", b)
	}
}

func TestHostStatsParsesMeminfo(t *testing.T) {
	root, _ := fakeHierarchy(t)
	proc := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proc, "pressure"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "pressure", "memory"),
		[]byte("some avg10=2.50 avg60=1.00 avg300=0.10 total=9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meminfo := "MemTotal:       16384000 kB\nMemFree:         1024000 kB\n" +
		"MemAvailable:    8192000 kB\nSwapTotal:      10485760 kB\nSwapFree:        9437184 kB\n"
	if err := os.WriteFile(filepath.Join(proc, "meminfo"), []byte(meminfo), 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewCgroups(root, proc).HostStats()
	if h.MemPressure != 2.50 {
		t.Errorf("MemPressure = %v", h.MemPressure)
	}
	if h.MemTotalBytes != 16384000*1024 {
		t.Errorf("MemTotalBytes = %d", h.MemTotalBytes)
	}
	if h.SwapTotalBytes != 10485760*1024 {
		t.Errorf("SwapTotalBytes = %d", h.SwapTotalBytes)
	}
	if u := h.MemUtilization(); u < 0.49 || u > 0.51 {
		t.Errorf("MemUtilization = %v, want ~0.5", u)
	}
}

func TestTierForRespectsPerAppOverride(t *testing.T) {
	d := NewDockerDriver()

	if got := d.tierFor(AppSpec{}); got.MemoryMaxMB != DefaultTier.MemoryMaxMB {
		t.Fatalf("default tier max = %d", got.MemoryMaxMB)
	}
	// A paid resource profile raises the ceiling, and the soft cap must follow
	// it — otherwise a 1 GB app throttles at the 192 MB default forever.
	got := d.tierFor(AppSpec{MemoryMB: 1024})
	if got.MemoryMaxMB != 1024 {
		t.Fatalf("MemoryMaxMB = %d, want 1024", got.MemoryMaxMB)
	}
	if got.MemoryHighMB != 512 {
		t.Fatalf("MemoryHighMB = %d, want 512", got.MemoryHighMB)
	}
}

// memory.high without swap is not a partial improvement — it is strictly worse
// than no soft cap, because the kernel cannot contain a cgroup it has nowhere
// to page out to and the pressure escalates into a system-wide OOM. ApplyQoS
// must refuse it while still applying the knobs that are safe.
func TestApplyQoSSkipsSoftCapWithoutSwap(t *testing.T) {
	root, cid := fakeHierarchy(t)
	proc := t.TempDir()
	// meminfo reporting SwapTotal: 0 — the pre-host-density.sh state.
	if err := os.WriteFile(filepath.Join(proc, "meminfo"),
		[]byte("MemTotal: 16384000 kB\nMemAvailable: 8192000 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	cg := NewCgroups(root, proc)
	if cg.HasSwap() {
		t.Fatal("precondition: HasSwap should be false")
	}

	dir := filepath.Join(root, "system.slice", "docker-"+cid+".scope")
	// Start from a known value so "unchanged" is distinguishable from "written".
	if err := os.WriteFile(filepath.Join(dir, "memory.high"), []byte("max"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cg.ApplyQoS(cid, 0, QoSTier{MemoryHighMB: 192, MemoryMaxMB: 384, CPUWeight: 100}); err != nil {
		t.Fatalf("ApplyQoS: %v", err)
	}

	high, err := os.ReadFile(filepath.Join(dir, "memory.high"))
	if err != nil {
		t.Fatal(err)
	}
	if string(high) != "max" {
		t.Fatalf("memory.high = %q — soft cap applied despite no swap", high)
	}
	// The hard cap is still safe and must be applied.
	max, err := os.ReadFile(filepath.Join(dir, "memory.max"))
	if err != nil {
		t.Fatal(err)
	}
	if string(max) != "402653184" {
		t.Fatalf("memory.max = %q, want the hard cap applied", max)
	}
}

// Reclaim must refuse outright without swap rather than appear to succeed.
func TestReclaimRefusedWithoutSwap(t *testing.T) {
	root, cid := fakeHierarchy(t)
	proc := t.TempDir()
	if err := os.WriteFile(filepath.Join(proc, "meminfo"),
		[]byte("MemTotal: 16384000 kB\nSwapTotal: 0 kB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "system.slice", "docker-"+cid+".scope")
	if err := os.WriteFile(filepath.Join(dir, "memory.reclaim"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	err := NewCgroups(root, proc).Reclaim(cid, 0, 512<<20)
	if err == nil {
		t.Fatal("expected Reclaim to refuse without swap")
	}
	if !strings.Contains(err.Error(), "no swap") {
		t.Fatalf("error = %v, want it to name the missing prerequisite", err)
	}
}
