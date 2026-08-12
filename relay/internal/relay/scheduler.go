package relay

// scheduler.go — node selection (Çok-node planı, Faz A).
//
// The single most valuable thing to do while still on one box.
//
// Today placement is a constant: `RUNNER_URL` is one string, and every call
// site reaches the runner through it. That works perfectly until the moment a
// second node exists — at which point every one of those call sites becomes a
// separate edit, in a codebase that by then is running thousands of live apps.
//
// So the string becomes an interface now, while it is free:
//
//	BUGÜN:  RUNNER_URL = "http://tunr-runner:9091"      (tek string)
//	HEDEF:  nodes tablosu + Scheduler.Pick(app) -> NodeClient
//
// With one node registered, Pick always returns the same client and nothing
// about today's behaviour changes. The point is that the *shape* is right: when
// worker #2 joins, the change is confined to this file and the nodes table.
//
// ── Pool, not shard ──
// Apps have no permanent home. `current_node_id` records where an app is running
// *right now*, not where it belongs. Because ~95% of apps are asleep at any
// moment — 0 RAM, just a file — deciding where one wakes up is nearly free, and
// adding or removing a node doesn't disturb the sleeping population. Pick is
// therefore called on every wake, not once at deploy.

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ahmetvural79/tunr/relay/internal/logger"
)

// NodeClient is everything the control plane needs from a node. RunnerClient
// implements it; a future remote agent implements the same surface.
//
// Deliberately narrow: no routes, no subdomains, no HTTP semantics. A node
// manages compute units and reports its own health, nothing more.
type NodeClient interface {
	// Wake makes an app dialable and returns its current IP (which changes
	// across a cold restart, so callers must re-target on the result).
	Wake(ctx context.Context, appID string) (ip string, err error)
	Sleep(ctx context.Context, appID string) error
	Stop(ctx context.Context, appID string) error
	Delete(ctx context.Context, appID string) error
	// Status reports the app's actual state ("running"|"sleeping"|"stopped").
	Status(ctx context.Context, appID string) (string, error)
	// Logs streams the app's output. Unlike every other call here it is not a
	// lifecycle operation and must not carry a lifecycle deadline: with
	// follow=true it is meant to stay open until the caller's ctx ends.
	Logs(ctx context.Context, appID string, tail int, follow bool) (io.ReadCloser, error)
	// HostSample reports the node's capacity and pressure — the input to both
	// the sweeper's safety valve and (later) the placement score.
	HostSample(ctx context.Context) (HostSample, error)
	Enabled() bool
}

// Node is a cluster member plus the client that reaches it.
type Node struct {
	ID     string
	Role   string // all | worker | builder | edge | data
	Region string
	URL    string
	// CPUBaseline is the CPU feature set this node guarantees. A gVisor
	// snapshot can only be restored on a machine that has every feature the
	// machine it was taken on had, so a checkpointed app must never be placed
	// on a node with a narrower baseline. Unused until Faz 2 — recorded now
	// because retrofitting it invalidates every snapshot in existence.
	CPUBaseline string
	Client      NodeClient
}

// Scheduler resolves "which node should serve this app".
//
// Faz A is deliberately the trivial implementation: one node, always chosen.
// The scoring function from the plan (content locality > volume affinity >
// free RAM > base-image affinity, minus PSI and tenant concentration) lands in
// Faz C, when there is more than one candidate for it to choose between.
type Scheduler struct {
	mu    sync.RWMutex
	nodes []*Node
}

// NewScheduler builds a scheduler over a fixed node set.
func NewScheduler(nodes ...*Node) *Scheduler {
	return &Scheduler{nodes: nodes}
}

// NewSingleNodeScheduler wraps today's single runner as the cluster's only
// member. This is the Faz A shape: the call sites are already going through the
// interface, so growing the cluster later doesn't touch them.
func NewSingleNodeScheduler(id, region, url, cpuBaseline string, c NodeClient) *Scheduler {
	if id == "" {
		id = "n_local_01"
	}
	return NewScheduler(&Node{
		ID: id, Role: "all", Region: region, URL: url,
		CPUBaseline: cpuBaseline, Client: c,
	})
}

// SetNodes replaces the node set (used when the nodes table is reloaded).
func (s *Scheduler) SetNodes(nodes []*Node) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes = nodes
}

// Nodes returns a snapshot of the cluster.
func (s *Scheduler) Nodes() []*Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]*Node(nil), s.nodes...)
}

// ErrNoCapacity is returned when no node can take the app.
//
// Admission control matters more than it looks: when the cluster is genuinely
// full, refusing is the honest answer. Silently oversubscribing is the road to
// an OOM that takes down apps belonging to people who did nothing wrong — and
// a refusal is also the signal that it's time to add a node.
var ErrNoCapacity = fmt.Errorf("no node with capacity for this app")

// Pick chooses a node to run an app on.
//
// Called on every wake, not once at deploy: apps are homeless by design, so
// placement is a fresh decision each time one comes back to life.
func (s *Scheduler) Pick(_ context.Context, appID string) (*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, n := range s.nodes {
		if n.Client == nil || !n.Client.Enabled() {
			continue
		}
		if n.Role != "all" && n.Role != "worker" {
			continue
		}
		return n, nil
	}
	return nil, fmt.Errorf("%w (app %s)", ErrNoCapacity, appID)
}

// ClientFor returns the node client for an app, or an error if none can serve it.
func (s *Scheduler) ClientFor(ctx context.Context, appID string) (NodeClient, error) {
	n, err := s.Pick(ctx, appID)
	if err != nil {
		return nil, err
	}
	return n.Client, nil
}

// ---------- Waker / appSleeper / hostSampler adapters ----------
//
// These let the existing relay components (CloudUpstream's Waker, the sweeper's
// appSleeper and hostSampler) talk to the scheduler without knowing it exists.
// That indirection is the entire deliverable of this file: when placement stops
// being a constant, nothing upstream of here has to change.

// Wake implements Waker: resolve the node, then wake there.
func (s *Scheduler) Wake(ctx context.Context, appID string) (string, error) {
	c, err := s.ClientFor(ctx, appID)
	if err != nil {
		return "", err
	}
	return c.Wake(ctx, appID)
}

// Sleep implements appSleeper.
func (s *Scheduler) Sleep(ctx context.Context, appID string) error {
	c, err := s.ClientFor(ctx, appID)
	if err != nil {
		return err
	}
	return c.Sleep(ctx, appID)
}

// Stop implements appSleeper.
func (s *Scheduler) Stop(ctx context.Context, appID string) error {
	c, err := s.ClientFor(ctx, appID)
	if err != nil {
		return err
	}
	return c.Stop(ctx, appID)
}

// Status reports an app's actual state on whichever node holds it.
func (s *Scheduler) Status(ctx context.Context, appID string) (string, error) {
	c, err := s.ClientFor(ctx, appID)
	if err != nil {
		return "", err
	}
	return c.Status(ctx, appID)
}

// Logs streams an app's output from whichever node holds it.
func (s *Scheduler) Logs(ctx context.Context, appID string, tail int, follow bool) (io.ReadCloser, error) {
	c, err := s.ClientFor(ctx, appID)
	if err != nil {
		return nil, err
	}
	return c.Logs(ctx, appID, tail, follow)
}

// ReconcileStates corrects the relay's beliefs against what the nodes report.
//
// Run once at startup. Every upstream is constructed as "awake", but the
// containers may be paused or stopped from before the restart — and because a
// paused container still completes a TCP handshake, probing can't tell. Without
// this pass the first request to such an app is proxied into a frozen process
// and hangs until the response timeout instead of waking it.
//
// Best-effort: an app whose status can't be read keeps its default, which is
// the pre-reconcile behaviour.
func (s *Scheduler) ReconcileStates(ctx context.Context, store *RouteStore) {
	if store == nil || !s.Enabled() {
		return
	}
	type target struct {
		appID string
		up    *CloudUpstream
	}
	var targets []target
	store.Each(func(_ string, up *CloudUpstream) {
		targets = append(targets, target{appID: up.AppID, up: up})
	})

	corrected := 0
	for _, t := range targets {
		if ctx.Err() != nil {
			return
		}
		sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		status, err := s.Status(sctx, t.appID)
		cancel()
		if err != nil {
			logger.Debug("reconcile %s: %v", t.appID, err)
			continue
		}
		var want SleepState
		switch status {
		case "sleeping":
			want = SleepWarm
		case "stopped":
			want = SleepStopped
		default:
			continue // running — the default is already correct
		}
		t.up.SetSleepState(want)
		corrected++
	}
	if corrected > 0 {
		logger.Info("reconcile: corrected %d/%d app state(s) from node truth", corrected, len(targets))
	}
}

// Enabled reports whether any node can currently serve.
func (s *Scheduler) Enabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, n := range s.nodes {
		if n.Client != nil && n.Client.Enabled() {
			return true
		}
	}
	return false
}

// HostSample implements hostSampler by aggregating the cluster.
//
// Aggregation rule: capacity sums, pressure takes the MAXIMUM. A cluster where
// one node is stalling and three are idle is a cluster with a problem — averaging
// that away would leave the safety valve disarmed exactly when it's needed.
func (s *Scheduler) HostSample(ctx context.Context) (HostSample, error) {
	nodes := s.Nodes()

	var agg HostSample
	var lastErr error
	ok := 0
	for _, n := range nodes {
		if n.Client == nil || !n.Client.Enabled() {
			continue
		}
		h, err := n.Client.HostSample(ctx)
		if err != nil {
			lastErr = err
			continue
		}
		ok++
		agg.MemTotalBytes += h.MemTotalBytes
		agg.MemAvailBytes += h.MemAvailBytes
		agg.SwapTotalBytes += h.SwapTotalBytes
		agg.SwapFreeBytes += h.SwapFreeBytes
		agg.MemPressure = maxFloat(agg.MemPressure, h.MemPressure)
		agg.CPUPressure = maxFloat(agg.CPUPressure, h.CPUPressure)
		agg.IOPressure = maxFloat(agg.IOPressure, h.IOPressure)
	}
	if ok == 0 {
		if lastErr == nil {
			lastErr = fmt.Errorf("no reachable nodes")
		}
		return HostSample{}, lastErr
	}
	if lastErr != nil {
		logger.Warn("scheduler: %d/%d nodes unreachable: %v", len(nodes)-ok, len(nodes), lastErr)
	}
	return agg, nil
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// Compile-time proof that the indirection actually holds. If someone adds a
// method to NodeClient, or changes what the relay expects of a waker/sleeper,
// this fails at build time rather than at 3am on a node that won't wake.
var (
	_ NodeClient  = (*RunnerClient)(nil)
	_ Waker       = (*Scheduler)(nil)
	_ appSleeper  = (*Scheduler)(nil)
	_ hostSampler = (*Scheduler)(nil)
)
