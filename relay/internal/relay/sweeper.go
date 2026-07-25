package relay

// sweeper.go — scale-to-zero (Yoğunluk planı Faz 0 + Faz 1).
//
// The state machine this drives:
//
//	HOT --idleSleep--> WARM --idleStop--> STOPPED
//	 ▲                  │                    │
//	 └───── request ─────┴────────────────────┘
//
// WARM is the state that pays for the whole design: `docker pause` freezes the
// container and the runner immediately asks the kernel to reclaim its now-cold
// pages into zram, so a sleeping app holds ~15-25 MB of real RAM instead of a
// full ~70 MB resident set. Resume is a decompress, not a boot.
//
// Two things this sweeper must never do:
//
//  1. Sleep an app with an open WebSocket/SSE connection. Freezing a container
//     mid-stream hangs every attached client, and the client has no way to tell
//     that from a network fault.
//  2. Keep cooling politely while the host is running out of memory. The PSI
//     safety valve exists because the alternative to shedding early is the
//     kernel OOM-killing something at random — quite possibly the relay.

import (
	"context"
	"time"

	"github.com/ahmetvural79/tunr/relay/internal/logger"
)

// appSleeper is the subset of RunnerClient the sweeper needs.
type appSleeper interface {
	Sleep(ctx context.Context, appID string) error
	Stop(ctx context.Context, appID string) error
	Enabled() bool
}

// hostSampler reports whole-box pressure. Optional: without it the sweeper runs
// on fixed thresholds and simply never enters aggressive mode.
type hostSampler interface {
	HostSample(ctx context.Context) (HostSample, error)
}

// HostSample is the relay-side view of the runner's host telemetry.
type HostSample struct {
	MemPressure    float64 `json:"mem_pressure"`
	CPUPressure    float64 `json:"cpu_pressure"`
	IOPressure     float64 `json:"io_pressure"`
	MemTotalBytes  uint64  `json:"mem_total_bytes"`
	MemAvailBytes  uint64  `json:"mem_avail_bytes"`
	SwapTotalBytes uint64  `json:"swap_total_bytes"`
	SwapFreeBytes  uint64  `json:"swap_free_bytes"`
}

// MemUtilization is the fraction of RAM in use (0..1), 0 when unknown.
func (h HostSample) MemUtilization() float64 {
	if h.MemTotalBytes == 0 {
		return 0
	}
	return 1 - float64(h.MemAvailBytes)/float64(h.MemTotalBytes)
}

// SweeperConfig tunes the idle policy.
type SweeperConfig struct {
	// IdleSleep is HOT→WARM. The plan drops this from 5m to ~45s: with reclaim
	// making sleep nearly free, holding an idle app hot for five minutes buys
	// nothing and costs a full resident set.
	IdleSleep time.Duration
	// IdleStop is WARM→STOPPED. 0 disables cold-stopping entirely.
	IdleStop time.Duration
	// Interval is the sweep period.
	Interval time.Duration

	// MemPressureThreshold is the PSI "some avg10" percentage above which the
	// sweeper enters aggressive mode.
	MemPressureThreshold float64
	// MemUtilThreshold is the used-RAM fraction (0..1) that also trips it —
	// PSI only rises once the kernel is already working hard, so a plain
	// utilisation ceiling catches the ramp earlier.
	MemUtilThreshold float64
	// AggressiveFactor divides both idle thresholds while under pressure.
	AggressiveFactor int
}

// DefaultSweeperConfig is the Faz 1 policy from the plan (§1.5).
func DefaultSweeperConfig() SweeperConfig {
	return SweeperConfig{
		IdleSleep:            45 * time.Second,
		IdleStop:             20 * time.Minute,
		Interval:             30 * time.Second,
		MemPressureThreshold: 10.0,
		MemUtilThreshold:     0.85,
		AggressiveFactor:     4,
	}
}

// StartIdleSweeper runs the scale-to-zero loop until ctx is cancelled.
// host may be nil — the safety valve is then simply never armed.
func StartIdleSweeper(ctx context.Context, store *RouteStore, r appSleeper, host hostSampler, cfg SweeperConfig) {
	if store == nil || r == nil || !r.Enabled() {
		return
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.AggressiveFactor < 1 {
		cfg.AggressiveFactor = 1
	}

	go func() {
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()
		aggressive := false

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			sleepAfter, stopAfter := cfg.IdleSleep, cfg.IdleStop
			if nowAggressive := underPressure(ctx, host, cfg); nowAggressive {
				sleepAfter /= time.Duration(cfg.AggressiveFactor)
				if stopAfter > 0 {
					stopAfter /= time.Duration(cfg.AggressiveFactor)
				}
				if !aggressive {
					logger.Warn("idle-sweep: memory pressure — cooling aggressively (sleep after %s, stop after %s)",
						sleepAfter.Round(time.Second), stopAfter.Round(time.Second))
				}
				aggressive = true
			} else {
				if aggressive {
					logger.Info("idle-sweep: pressure cleared — back to normal thresholds")
				}
				aggressive = false
			}

			sweepOnce(ctx, store, r, sleepAfter, stopAfter)
		}
	}()
}

// underPressure reports whether the host is short enough on memory to justify
// shedding warm apps early.
func underPressure(ctx context.Context, host hostSampler, cfg SweeperConfig) bool {
	if host == nil {
		return false
	}
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	h, err := host.HostSample(sctx)
	if err != nil {
		return false // telemetry down is not evidence of pressure
	}
	if cfg.MemPressureThreshold > 0 && h.MemPressure >= cfg.MemPressureThreshold {
		return true
	}
	if cfg.MemUtilThreshold > 0 && h.MemUtilization() >= cfg.MemUtilThreshold {
		return true
	}
	return false
}

// sweepOnce walks every cloud route once and applies the idle policy.
func sweepOnce(ctx context.Context, store *RouteStore, r appSleeper, sleepAfter, stopAfter time.Duration) {
	now := time.Now()

	// Collect first, act after: Each holds a read lock on the store, and the
	// runner calls below are network round-trips. Doing them under the lock
	// would block every incoming request's route lookup for the whole sweep.
	type candidate struct {
		up   *CloudUpstream
		idle time.Duration
	}
	var cands []candidate
	store.Each(func(_ string, up *CloudUpstream) {
		last := up.LastSeen()
		if last.IsZero() {
			return // never served yet — leave freshly-deployed apps up
		}
		cands = append(cands, candidate{up: up, idle: now.Sub(last)})
	})

	for _, c := range cands {
		if ctx.Err() != nil {
			return
		}
		up, idle := c.up, c.idle

		// An open WebSocket/SSE connection forbids sleep regardless of how long
		// ago the *request* started — a streaming connection is one old request
		// by the idle clock's reckoning, but the client is very much still there.
		if up.Pinned() {
			continue
		}

		switch state := up.SleepState(); {
		case stopAfter > 0 && idle >= stopAfter && state != SleepStopped:
			if err := r.Stop(ctx, up.AppID); err != nil {
				logger.Warn("idle-sweep: stop %s: %v", up.AppID, err)
				continue
			}
			up.SetSleepState(SleepStopped)
			logger.Info("idle-sweep: stopped %s (idle %s)", up.AppID, idle.Round(time.Second))

		case idle >= sleepAfter && state == SleepAwake:
			// Sleep = pause + reclaim on the runner side. That's where the
			// ~70 MB → ~20 MB drop actually happens.
			if err := r.Sleep(ctx, up.AppID); err != nil {
				logger.Warn("idle-sweep: sleep %s: %v", up.AppID, err)
				continue
			}
			up.SetSleepState(SleepWarm)
			logger.Info("idle-sweep: warm %s (idle %s)", up.AppID, idle.Round(time.Second))
		}
	}
}
