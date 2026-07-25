package relay

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeNode is a NodeClient stub.
type fakeNode struct {
	enabled bool
	ip      string
	sample  HostSample
	err     error
	status  string

	woke, slept, stopped, deleted int
}

func (f *fakeNode) Status(context.Context, string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if f.status == "" {
		return "running", nil
	}
	return f.status, nil
}

func (f *fakeNode) Wake(context.Context, string) (string, error) {
	f.woke++
	return f.ip, f.err
}
func (f *fakeNode) Sleep(context.Context, string) error  { f.slept++; return f.err }
func (f *fakeNode) Stop(context.Context, string) error   { f.stopped++; return f.err }
func (f *fakeNode) Delete(context.Context, string) error { f.deleted++; return f.err }
func (f *fakeNode) HostSample(context.Context) (HostSample, error) {
	return f.sample, f.err
}
func (f *fakeNode) Enabled() bool { return f.enabled }

func TestSchedulerPickSingleNode(t *testing.T) {
	c := &fakeNode{enabled: true}
	s := NewSingleNodeScheduler("n_local_01", "ams", "http://runner:9091", "x86-64-v2", c)

	n, err := s.Pick(context.Background(), "a_1")
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if n.ID != "n_local_01" || n.Client != c {
		t.Fatalf("picked %+v", n)
	}
	if n.CPUBaseline != "x86-64-v2" {
		t.Fatalf("CPUBaseline = %q", n.CPUBaseline)
	}
}

// With no usable node, placement must fail loudly. Silently oversubscribing is
// how a box walks into an OOM that kills apps belonging to people who did
// nothing wrong — and a refusal is also the signal to add capacity.
func TestSchedulerPickNoCapacity(t *testing.T) {
	s := NewSingleNodeScheduler("n1", "ams", "http://x", "x86-64-v2", &fakeNode{enabled: false})

	if _, err := s.Pick(context.Background(), "a_1"); !errors.Is(err, ErrNoCapacity) {
		t.Fatalf("err = %v, want ErrNoCapacity", err)
	}
	if s.Enabled() {
		t.Fatal("Enabled() true with no usable node")
	}
}

// Only nodes that can run apps are placement candidates — a builder-only node
// has no gVisor sandboxes and would accept work it cannot perform.
func TestSchedulerPickSkipsNonWorkerRoles(t *testing.T) {
	builder := &fakeNode{enabled: true}
	worker := &fakeNode{enabled: true}
	s := NewScheduler(
		&Node{ID: "n_build", Role: "builder", Client: builder},
		&Node{ID: "n_work", Role: "worker", Client: worker},
	)

	n, err := s.Pick(context.Background(), "a_1")
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if n.ID != "n_work" {
		t.Fatalf("picked %s, want the worker", n.ID)
	}
}

func TestSchedulerDelegatesLifecycle(t *testing.T) {
	c := &fakeNode{enabled: true, ip: "172.20.0.9"}
	s := NewSingleNodeScheduler("n1", "ams", "http://x", "x86-64-v2", c)
	ctx := context.Background()

	ip, err := s.Wake(ctx, "a_1")
	if err != nil || ip != "172.20.0.9" {
		t.Fatalf("Wake = %q, %v", ip, err)
	}
	if err := s.Sleep(ctx, "a_1"); err != nil {
		t.Fatalf("Sleep: %v", err)
	}
	if err := s.Stop(ctx, "a_1"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if c.woke != 1 || c.slept != 1 || c.stopped != 1 {
		t.Fatalf("delegation counts: woke=%d slept=%d stopped=%d", c.woke, c.slept, c.stopped)
	}
}

// Capacity sums, but pressure takes the MAXIMUM. A cluster where one node is
// stalling and three are idle has a problem; averaging it away would leave the
// sweeper's safety valve disarmed exactly when it matters.
func TestSchedulerHostSampleAggregates(t *testing.T) {
	a := &fakeNode{enabled: true, sample: HostSample{
		MemTotalBytes: 16 << 30, MemAvailBytes: 8 << 30, MemPressure: 1.0, CPUPressure: 40.0,
	}}
	b := &fakeNode{enabled: true, sample: HostSample{
		MemTotalBytes: 32 << 30, MemAvailBytes: 30 << 30, MemPressure: 55.0, CPUPressure: 2.0,
	}}
	s := NewScheduler(
		&Node{ID: "n1", Role: "worker", Client: a},
		&Node{ID: "n2", Role: "worker", Client: b},
	)

	got, err := s.HostSample(context.Background())
	if err != nil {
		t.Fatalf("HostSample: %v", err)
	}
	if got.MemTotalBytes != 48<<30 {
		t.Errorf("MemTotalBytes = %d, want 48GiB", got.MemTotalBytes)
	}
	if got.MemAvailBytes != 38<<30 {
		t.Errorf("MemAvailBytes = %d, want 38GiB", got.MemAvailBytes)
	}
	if got.MemPressure != 55.0 {
		t.Errorf("MemPressure = %v, want the max (55)", got.MemPressure)
	}
	if got.CPUPressure != 40.0 {
		t.Errorf("CPUPressure = %v, want the max (40)", got.CPUPressure)
	}
}

// One unreachable node must not blind the whole sample — the reachable ones
// still carry the pressure signal.
func TestSchedulerHostSamplePartialFailure(t *testing.T) {
	good := &fakeNode{enabled: true, sample: HostSample{MemTotalBytes: 16 << 30, MemPressure: 12}}
	bad := &fakeNode{enabled: true, err: errors.New("connection refused")}
	s := NewScheduler(
		&Node{ID: "n1", Role: "worker", Client: good},
		&Node{ID: "n2", Role: "worker", Client: bad},
	)

	got, err := s.HostSample(context.Background())
	if err != nil {
		t.Fatalf("HostSample: %v", err)
	}
	if got.MemTotalBytes != 16<<30 || got.MemPressure != 12 {
		t.Fatalf("got %+v, want the reachable node's sample", got)
	}
}

// If nothing is reachable we must return an error, not a zero sample — a zero
// sample reads as "0% pressure, plenty of RAM", which would tell the safety
// valve everything is fine at the worst possible moment.
func TestSchedulerHostSampleAllUnreachable(t *testing.T) {
	s := NewScheduler(&Node{ID: "n1", Role: "worker", Client: &fakeNode{
		enabled: true, err: errors.New("down"),
	}})

	if _, err := s.HostSample(context.Background()); err == nil {
		t.Fatal("expected an error when every node is unreachable")
	}
}

// After a relay restart every upstream defaults to "awake". If the container
// is actually paused, a TCP probe still succeeds (the kernel completes the
// handshake), so the relay would proxy into a frozen process and hang. The
// reconcile pass must correct that from node truth.
func TestSchedulerReconcileStatesCorrectsPausedApp(t *testing.T) {
	c := &fakeNode{enabled: true, status: "sleeping"}
	s := NewSingleNodeScheduler("n1", "ams", "http://x", "x86-64-v2", c)

	store := NewRouteStore()
	up := newTestUpstream("a_paused", time.Minute)
	store.SetCloud("paused", up)
	if up.SleepState() != SleepAwake {
		t.Fatal("precondition: upstream should start awake")
	}

	s.ReconcileStates(context.Background(), store)

	if up.SleepState() != SleepWarm {
		t.Fatalf("state = %v after reconcile, want SleepWarm", up.SleepState())
	}
}

func TestSchedulerReconcileStatesMapsStopped(t *testing.T) {
	s := NewSingleNodeScheduler("n1", "ams", "http://x", "x86-64-v2",
		&fakeNode{enabled: true, status: "stopped"})

	store := NewRouteStore()
	up := newTestUpstream("a_cold", time.Minute)
	store.SetCloud("cold", up)

	s.ReconcileStates(context.Background(), store)

	if up.SleepState() != SleepStopped {
		t.Fatalf("state = %v, want SleepStopped", up.SleepState())
	}
}

// An unreadable status must leave the default in place rather than guessing —
// a wrong correction is worse than no correction.
func TestSchedulerReconcileStatesToleratesUnreachableNode(t *testing.T) {
	s := NewSingleNodeScheduler("n1", "ams", "http://x", "x86-64-v2",
		&fakeNode{enabled: true, err: errors.New("connection refused")})

	store := NewRouteStore()
	up := newTestUpstream("a_1", time.Minute)
	store.SetCloud("one", up)

	s.ReconcileStates(context.Background(), store)

	if up.SleepState() != SleepAwake {
		t.Fatalf("state = %v, want the default SleepAwake", up.SleepState())
	}
}

func TestSchedulerSetNodes(t *testing.T) {
	s := NewSingleNodeScheduler("n1", "ams", "http://x", "x86-64-v2", &fakeNode{enabled: true})
	if len(s.Nodes()) != 1 {
		t.Fatalf("expected 1 node")
	}

	s.SetNodes([]*Node{
		{ID: "n2", Role: "worker", Client: &fakeNode{enabled: true}},
		{ID: "n3", Role: "worker", Client: &fakeNode{enabled: true}},
	})
	if len(s.Nodes()) != 2 {
		t.Fatalf("expected 2 nodes after SetNodes")
	}
	n, err := s.Pick(context.Background(), "a_1")
	if err != nil || n.ID != "n2" {
		t.Fatalf("Pick after SetNodes = %v, %v", n, err)
	}
}
