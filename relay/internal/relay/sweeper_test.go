package relay

import (
	"context"
	"net/url"
	"sync"
	"testing"
	"time"
)

// fakeSleeper records what the sweeper asked the runner to do.
type fakeSleeper struct {
	mu      sync.Mutex
	slept   []string
	stopped []string
	err     error
}

func (f *fakeSleeper) Sleep(_ context.Context, appID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.slept = append(f.slept, appID)
	return nil
}

func (f *fakeSleeper) Stop(_ context.Context, appID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.stopped = append(f.stopped, appID)
	return nil
}

func (f *fakeSleeper) Enabled() bool { return true }

func (f *fakeSleeper) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.slept), len(f.stopped)
}

// newTestUpstream builds an upstream whose last request was idleAgo in the past.
func newTestUpstream(appID string, idleAgo time.Duration) *CloudUpstream {
	u, _ := url.Parse("http://127.0.0.1:9/")
	up := NewCloudUpstream(appID, u, []byte("s"), nil, nil)
	up.Metrics = NewWakeMetrics()
	if idleAgo >= 0 {
		up.lastSeenMu.Lock()
		up.lastSeen = time.Now().Add(-idleAgo)
		up.lastSeenMu.Unlock()
	}
	return up
}

func TestSweepOnceMovesIdleAppToWarm(t *testing.T) {
	store := NewRouteStore()
	up := newTestUpstream("a_idle", 2*time.Minute)
	store.SetCloud("idle", up)

	f := &fakeSleeper{}
	sweepOnce(context.Background(), store, f, 45*time.Second, 20*time.Minute)

	slept, stopped := f.counts()
	if slept != 1 || stopped != 0 {
		t.Fatalf("slept=%d stopped=%d, want 1/0", slept, stopped)
	}
	if up.SleepState() != SleepWarm {
		t.Fatalf("state = %v, want SleepWarm", up.SleepState())
	}
}

func TestSweepOnceStopsVeryIdleApp(t *testing.T) {
	store := NewRouteStore()
	up := newTestUpstream("a_cold", 3*time.Hour)
	up.SetSleepState(SleepWarm) // already warm from an earlier sweep
	store.SetCloud("cold", up)

	f := &fakeSleeper{}
	sweepOnce(context.Background(), store, f, 45*time.Second, 20*time.Minute)

	slept, stopped := f.counts()
	if slept != 0 || stopped != 1 {
		t.Fatalf("slept=%d stopped=%d, want 0/1", slept, stopped)
	}
	if up.SleepState() != SleepStopped {
		t.Fatalf("state = %v, want SleepStopped", up.SleepState())
	}
}

// The core safety invariant: freezing a container with an open WebSocket/SSE
// connection hangs every attached client, and the client cannot distinguish
// that from a network fault. A pinned app must never be slept, however idle
// its last *request* timestamp looks.
func TestSweepOnceNeverSleepsPinnedApp(t *testing.T) {
	store := NewRouteStore()
	up := newTestUpstream("a_streaming", 6*time.Hour)
	up.pins.Add(1) // an open connection
	store.SetCloud("streaming", up)

	f := &fakeSleeper{}
	sweepOnce(context.Background(), store, f, 45*time.Second, 20*time.Minute)

	if slept, stopped := f.counts(); slept != 0 || stopped != 0 {
		t.Fatalf("pinned app was touched: slept=%d stopped=%d", slept, stopped)
	}
	if up.SleepState() != SleepAwake {
		t.Fatalf("state = %v, want SleepAwake", up.SleepState())
	}
}

// A freshly deployed app that has never served a request must stay up — its
// zero lastSeen would otherwise read as infinitely idle and put it straight to
// sleep before anyone could reach it.
func TestSweepOnceSkipsNeverServedApp(t *testing.T) {
	store := NewRouteStore()
	up := newTestUpstream("a_fresh", -1) // leave lastSeen zero
	store.SetCloud("fresh", up)

	f := &fakeSleeper{}
	sweepOnce(context.Background(), store, f, 45*time.Second, 20*time.Minute)

	if slept, stopped := f.counts(); slept != 0 || stopped != 0 {
		t.Fatalf("fresh app was touched: slept=%d stopped=%d", slept, stopped)
	}
}

// A busy app is left alone.
func TestSweepOnceLeavesActiveAppHot(t *testing.T) {
	store := NewRouteStore()
	up := newTestUpstream("a_busy", 5*time.Second)
	store.SetCloud("busy", up)

	f := &fakeSleeper{}
	sweepOnce(context.Background(), store, f, 45*time.Second, 20*time.Minute)

	if slept, stopped := f.counts(); slept != 0 || stopped != 0 {
		t.Fatalf("active app was touched: slept=%d stopped=%d", slept, stopped)
	}
}

// IdleStop=0 disables cold-stopping: apps should reach WARM and stay there.
func TestSweepOnceStopDisabled(t *testing.T) {
	store := NewRouteStore()
	up := newTestUpstream("a_warm", 30*24*time.Hour)
	up.SetSleepState(SleepWarm)
	store.SetCloud("warm", up)

	f := &fakeSleeper{}
	sweepOnce(context.Background(), store, f, 45*time.Second, 0)

	if _, stopped := f.counts(); stopped != 0 {
		t.Fatalf("stopped %d apps with IdleStop=0", stopped)
	}
}

// A runner error must not desync relay state from reality: if the Sleep call
// failed the app is still running, and recording it as WARM would make the next
// request skip the wake and proxy into a container that was never paused —
// or worse, never wake one that was.
func TestSweepOnceKeepsStateOnRunnerError(t *testing.T) {
	store := NewRouteStore()
	up := newTestUpstream("a_err", 2*time.Minute)
	store.SetCloud("err", up)

	f := &fakeSleeper{err: context.DeadlineExceeded}
	sweepOnce(context.Background(), store, f, 45*time.Second, 20*time.Minute)

	if up.SleepState() != SleepAwake {
		t.Fatalf("state = %v after failed sleep, want SleepAwake", up.SleepState())
	}
}

// ---------- pressure valve ----------

type fakeHost struct {
	sample HostSample
	err    error
}

func (f *fakeHost) HostSample(context.Context) (HostSample, error) { return f.sample, f.err }

func TestUnderPressure(t *testing.T) {
	cfg := DefaultSweeperConfig()

	cases := []struct {
		name string
		host hostSampler
		want bool
	}{
		{"no sampler", nil, false},
		{"calm box", &fakeHost{sample: HostSample{
			MemPressure: 0.5, MemTotalBytes: 16 << 30, MemAvailBytes: 10 << 30,
		}}, false},
		{"PSI above threshold", &fakeHost{sample: HostSample{
			MemPressure: 25, MemTotalBytes: 16 << 30, MemAvailBytes: 10 << 30,
		}}, true},
		{"utilization above threshold", &fakeHost{sample: HostSample{
			MemPressure: 0, MemTotalBytes: 16 << 30, MemAvailBytes: 1 << 30, // ~94% used
		}}, true},
		// Telemetry being down is not evidence that the box is fine, but it is
		// also not evidence of pressure — cooling every app because the runner
		// is briefly unreachable would be its own outage.
		{"sampler error", &fakeHost{err: context.DeadlineExceeded}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := underPressure(context.Background(), c.host, cfg); got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestHostSampleMemUtilization(t *testing.T) {
	h := HostSample{MemTotalBytes: 16 << 30, MemAvailBytes: 4 << 30}
	if got := h.MemUtilization(); got < 0.74 || got > 0.76 {
		t.Fatalf("utilization = %v, want ~0.75", got)
	}
	// Unknown total must not read as 100% used and trip the valve.
	if got := (HostSample{}).MemUtilization(); got != 0 {
		t.Fatalf("empty sample utilization = %v, want 0", got)
	}
}
