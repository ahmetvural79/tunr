package relay

// node_reporter.go — feeds the node_metrics time series (Çok-node planı, Faz A).
//
// The multi-node plan decides when to add hardware from thresholds, not from
// feel: "mem.hot_utilization > 70% (7-day p95) → add a WORKER",
// "build.queue_wait_p95 > 60s → add a BUILDER". Those thresholds are worthless
// without history behind them, and history can only be collected before you
// need it. So the reporter starts now, on one node, writing samples nobody
// reads yet — so that in three months the question "do we need another box?"
// has a data-backed answer instead of a guess.
//
// Everything here is best-effort. A reporting failure must never affect
// serving: this is a bookkeeping loop, and the data plane doesn't depend on it.

import (
	"context"
	"time"

	"github.com/ahmetvural79/tunr/relay/internal/db"
	"github.com/ahmetvural79/tunr/relay/internal/logger"
)

// NodeReporterConfig tunes the sampling loop.
type NodeReporterConfig struct {
	// Interval between samples. One minute gives usable percentiles without
	// making the table grow faster than it's pruned.
	Interval time.Duration
	// Retention is how far back samples are kept. The decision window the plan
	// uses is 7 days; 30 leaves room to look back at a past incident.
	Retention time.Duration
	// PruneEvery is how often old samples are deleted.
	PruneEvery time.Duration
}

// DefaultNodeReporterConfig returns sane sampling defaults.
func DefaultNodeReporterConfig() NodeReporterConfig {
	return NodeReporterConfig{
		Interval:   time.Minute,
		Retention:  30 * 24 * time.Hour,
		PruneEvery: 6 * time.Hour,
	}
}

// StartNodeReporter samples the cluster into node_metrics until ctx is done.
// A nil database disables it (in-memory mode) — the relay still serves.
func StartNodeReporter(ctx context.Context, database *db.DB, sched *Scheduler, store *RouteStore, cfg NodeReporterConfig) {
	if database == nil || sched == nil {
		return
	}
	if cfg.Interval <= 0 {
		cfg.Interval = time.Minute
	}

	// Register this box as a cluster member before sampling — node_metrics has
	// a foreign key onto nodes, so an unregistered node's samples would be
	// rejected row by row.
	for _, n := range sched.Nodes() {
		regCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := database.UpsertNode(regCtx, db.Node{
			ID: n.ID, Role: n.Role, Region: n.Region, URL: n.URL,
			Status: "ready", CPUBaseline: n.CPUBaseline,
		})
		cancel()
		if err != nil {
			logger.Warn("node register %s: %v", n.ID, err)
		} else {
			logger.Info("node registered: %s (role=%s region=%s baseline=%s)",
				n.ID, n.Role, n.Region, n.CPUBaseline)
		}
	}

	go func() {
		sample := time.NewTicker(cfg.Interval)
		defer sample.Stop()
		prune := time.NewTicker(cfg.PruneEvery)
		defer prune.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-sample.C:
				reportOnce(ctx, database, sched, store)
			case <-prune.C:
				pctx, cancel := context.WithTimeout(ctx, time.Minute)
				if n, err := database.PruneNodeMetrics(pctx, cfg.Retention); err != nil {
					logger.Warn("node_metrics prune: %v", err)
				} else if n > 0 {
					logger.Info("node_metrics: pruned %d sample(s) older than %s", n, cfg.Retention)
				}
				cancel()
			}
		}
	}()
}

// reportOnce writes one sample per reachable node.
func reportOnce(ctx context.Context, database *db.DB, sched *Scheduler, store *RouteStore) {
	// App-state counts come from the relay's own view rather than the runner's,
	// because the relay is the thing that decides those states — and it stays
	// accurate even when a node is briefly unreachable.
	hot, warm, stopped := countStates(store)

	for _, n := range sched.Nodes() {
		if n.Client == nil || !n.Client.Enabled() {
			continue
		}
		sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		h, err := n.Client.HostSample(sctx)
		cancel()
		if err != nil {
			logger.Debug("node sample %s: %v", n.ID, err)
			continue
		}

		wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
		err = database.RecordNodeMetrics(wctx, db.NodeMetricSample{
			NodeID:         n.ID,
			MemTotalBytes:  h.MemTotalBytes,
			MemAvailBytes:  h.MemAvailBytes,
			SwapTotalBytes: h.SwapTotalBytes,
			SwapUsedBytes:  h.SwapTotalBytes - h.SwapFreeBytes,
			MemPressure:    h.MemPressure,
			CPUPressure:    h.CPUPressure,
			IOPressure:     h.IOPressure,
			AppsHot:        hot,
			AppsWarm:       warm,
			AppsStopped:    stopped,
		})
		wcancel()
		if err != nil {
			logger.Warn("node_metrics write %s: %v", n.ID, err)
			continue
		}
		if err := database.Heartbeat(ctx, n.ID); err != nil {
			logger.Debug("node heartbeat %s: %v", n.ID, err)
		}
	}
}

// countStates tallies the relay's view of app lifecycle states.
func countStates(store *RouteStore) (hot, warm, stopped int) {
	if store == nil {
		return 0, 0, 0
	}
	store.Each(func(_ string, up *CloudUpstream) {
		switch up.SleepState() {
		case SleepWarm:
			warm++
		case SleepStopped:
			stopped++
		default:
			hot++
		}
	})
	return hot, warm, stopped
}
