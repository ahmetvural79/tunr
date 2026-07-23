package relay

// sweeper.go — scale-to-zero. Periodically pauses idle cloud apps (RAM stays
// resident, instant resume) and cold-stops the very idle ones. The next request
// wakes them: CloudUpstream is marked sleeping, so ServeHTTP wakes before proxy.

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

// StartIdleSweeper runs the scale-to-zero loop until ctx is cancelled.
func StartIdleSweeper(ctx context.Context, store *RouteStore, r appSleeper, idleSleep, idleStop time.Duration) {
	if store == nil || r == nil || !r.Enabled() {
		return
	}
	go func() {
		phase := map[string]int{} // appID -> 0 active, 1 slept, 2 stopped
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			now := time.Now()
			store.Each(func(_ string, up *CloudUpstream) {
				last := up.LastSeen()
				if last.IsZero() {
					return // never served yet — leave freshly-deployed apps up
				}
				idle := now.Sub(last)
				cur := phase[up.AppID]
				switch {
				case idleStop > 0 && idle >= idleStop && cur < 2:
					if err := r.Stop(ctx, up.AppID); err == nil {
						up.SetSleeping(true)
						phase[up.AppID] = 2
						logger.Info("idle-sweep: stopped %s (idle %s)", up.AppID, idle.Round(time.Minute))
					}
				case idle >= idleSleep && cur == 0:
					if err := r.Sleep(ctx, up.AppID); err == nil {
						up.SetSleeping(true)
						phase[up.AppID] = 1
						logger.Info("idle-sweep: slept %s (idle %s)", up.AppID, idle.Round(time.Minute))
					}
				case idle < idleSleep && cur != 0:
					phase[up.AppID] = 0 // woken again since last sweep
				}
			})
		}
	}()
}
