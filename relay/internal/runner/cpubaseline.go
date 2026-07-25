package runner

// cpubaseline.go — pinned CPU feature set for gVisor sandboxes
// (Çok-node planı, Faz A — the one item that is genuinely irreversible).
//
// ── Why this exists before it's needed ──
//
// gVisor's checkpoint/restore (Faz 2, the lever that takes a sleeping app to
// literally 0 RAM) has a hard constraint: a snapshot can only be restored on a
// machine that supports *every* CPU feature the machine it was taken on had.
// The application's own JIT-compiled and vectorised code is frozen inside that
// snapshot; restore it on a CPU missing one instruction and it dies with an
// Invalid Opcode the first time that path executes.
//
// Modal learned this the expensive way on AWS: an instance type that didn't
// support `pclmulqdq` broke restore for snapshots taken elsewhere.
//
// The fix is to advertise a *conservative baseline* to every sandbox, so
// snapshots are portable across any machine meeting that baseline rather than
// tied to the exact silicon that produced them.
//
// ── Why now, on one node ──
//
// Today this changes nothing: one machine, so every snapshot restores where it
// was taken. But the baseline is stamped into each snapshot at creation, which
// means CHANGING IT LATER INVALIDATES EVERY EXISTING SNAPSHOT. Setting it now
// costs an annotation. Setting it after Faz 2 ships, with thousands of
// checkpointed apps on disk, is a migration project.
//
// ── Why it ships DISABLED ──
//
// Verified on runsc release-20260721.0: passing
// `--annotation dev.gvisor.internal.cpufeatures=x86-64-v2` makes sandbox
// creation fail outright —
//
//	cannot create sandbox: cannot read client sync file:
//	waiting for sandbox to start: EOF
//
// — so every `docker run` for an app dies. The microarchitecture-level name is
// not the form this runsc build accepts, and `runsc flags` does not advertise
// the knob at all.
//
// The baseline buys nothing until checkpoint/restore exists (Faz 2), and until
// then an incorrect value costs 100% of deploys. So the plumbing is here and
// the default is off. Before turning it on:
//
//  1. Confirm the accepted syntax for the runsc build in use — it may be a
//     comma-separated feature list (e.g. "sse4_2,pclmulqdq,popcnt") rather
//     than a level name, or may need a newer release.
//  2. Verify a sandbox actually starts with it set.
//  3. Only then set TUNR_CPU_BASELINE, and treat the value as frozen —
//     changing it later invalidates every snapshot taken under the old one.
//
// x86-64-v2 remains the intended target (SSE3/SSSE3/SSE4.1/SSE4.2/POPCNT/
// CMPXCHG16B, plus the pclmulqdq that bit Modal): universal on server CPUs
// since ~2009, and low enough to keep older cloud hardware in the pool. Going
// to v3 (AVX2) would buy application performance at the cost of excluding
// machines, which is the wrong trade for a density-first product.

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// DefaultCPUBaseline is empty: the annotation is disabled until its syntax is
// verified against the deployed runsc build. See the note above.
const DefaultCPUBaseline = ""

// IntendedCPUBaseline is the value to adopt once the syntax is confirmed.
// Recorded here so the decision isn't lost between now and Faz 2.
const IntendedCPUBaseline = "x86-64-v2"

// gvisorCPUFeaturesAnnotation is the OCI annotation runsc reads to decide which
// CPU features to expose to the sandbox.
const gvisorCPUFeaturesAnnotation = "dev.gvisor.internal.cpufeatures"

// dockerAnnotationMinMajor is the first Docker Engine major version with
// `docker run --annotation`. On anything older the flag is a hard error, so it
// must be probed rather than assumed — a broken deploy is worse than an
// unpinned baseline.
const dockerAnnotationMinMajor = 25

var (
	annotationOnce sync.Once
	annotationOK   bool
)

// supportsAnnotations reports whether the daemon accepts --annotation.
// Probed once; a failed probe answers "no", which is the safe direction.
func supportsAnnotations(ctx context.Context) bool {
	annotationOnce.Do(func() {
		out, err := exec.CommandContext(ctx, "docker", "version", "-f", "{{.Server.Version}}").Output()
		if err != nil {
			return
		}
		major, _, _ := strings.Cut(strings.TrimSpace(string(out)), ".")
		n, err := strconv.Atoi(major)
		if err != nil {
			return
		}
		annotationOK = n >= dockerAnnotationMinMajor
	})
	return annotationOK
}

// cpuBaselineArgs returns the `docker run` flags that pin the sandbox's CPU
// feature set, or nil when the baseline is disabled or unsupported.
//
// Only meaningful under runsc — runc sandboxes see the host CPU directly and
// have no snapshot portability story to protect.
func (d *DockerDriver) cpuBaselineArgs(ctx context.Context) []string {
	baseline := d.CPUBaseline
	if baseline == "" || strings.EqualFold(baseline, "off") {
		return nil
	}
	if d.Runtime != "runsc" {
		return nil
	}
	if !supportsAnnotations(ctx) {
		logCPUBaselineUnsupported()
		return nil
	}
	return []string{"--annotation", gvisorCPUFeaturesAnnotation + "=" + baseline}
}

var baselineWarnOnce sync.Once

// logCPUBaselineUnsupported warns exactly once. This is a real future problem
// (snapshots taken here won't be portable) but not a present failure, so it must
// be visible without drowning every deploy log.
func logCPUBaselineUnsupported() {
	baselineWarnOnce.Do(func() {
		logQoS("CPU baseline NOT pinned: Docker Engine < %d has no --annotation support. "+
			"Fine on a single node; MUST be fixed before checkpoint/restore (Faz 2) or "+
			"snapshots will be tied to this exact machine.", dockerAnnotationMinMajor)
	})
}
