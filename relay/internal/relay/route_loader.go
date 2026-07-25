package relay

// route_loader.go — keeps a RouteStore in sync with the `routes` table.
//
// Primary path: Postgres LISTEN/NOTIFY (payload = changed subdomain) → reload
// that one row. On LISTEN drop: reconnect with backoff + a full reload to catch
// missed changes. Safety net: a periodic full reconcile. In in-memory mode
// (no DB) this is a no-op and the tunnel path is unaffected.

import (
	"context"
	"net/url"
	"time"

	"github.com/ahmetvural79/tunr/relay/internal/db"
	"github.com/ahmetvural79/tunr/relay/internal/logger"
)

// RouteLoader syncs cloud routes from Postgres into a RouteStore.
type RouteLoader struct {
	db     *db.DB
	store  *RouteStore
	waker  Waker
	freeze FreezeCache
	cache  *RouteCache
}

// NewRouteLoader wires a loader. waker/freeze may be nil (freeze arrives Faz 1).
func NewRouteLoader(database *db.DB, store *RouteStore, waker Waker, freeze FreezeCache) *RouteLoader {
	return &RouteLoader{db: database, store: store, waker: waker, freeze: freeze}
}

// WithCache enables the on-disk route mirror, so the relay can serve existing
// apps through a Postgres outage. See route_cache.go for why this matters.
func (l *RouteLoader) WithCache(c *RouteCache) *RouteLoader {
	l.cache = c
	return l
}

// Start does an initial load, then runs the LISTEN loop + reconcile poll.
func (l *RouteLoader) Start(ctx context.Context) {
	if l.store == nil {
		return
	}
	if l.db == nil {
		// No pool at all. Still serve whatever the last good sync produced —
		// running apps do not stop being reachable because the control plane's
		// database is gone.
		if n := l.cache.LoadInto(l.apply); n == 0 {
			logger.Info("route loader: no DB — cloud routes disabled (tunnel path unaffected)")
		}
		return
	}
	if err := l.reloadAll(ctx); err != nil {
		// The data plane does not depend on Postgres. If the initial load
		// fails, fall back to the local mirror so already-deployed apps keep
		// serving; only control-plane work (new deploys, route changes) stalls.
		logger.Warn("route loader initial load failed: %v", err)
		if n := l.cache.LoadInto(l.apply); n == 0 {
			logger.Warn("route loader: no local cache either — cloud apps will 404 until Postgres returns")
		}
	}
	go l.listenLoop(ctx)
	go l.pollLoop(ctx)
}

func (l *RouteLoader) apply(cr db.CloudRoute) {
	if cr.CloudURL == "" {
		logger.Warn("route %s: empty cloud_url, skipping", cr.Subdomain)
		return
	}
	target, err := url.Parse(cr.CloudURL)
	if err != nil {
		logger.Warn("route %s: bad cloud_url %q: %v", cr.Subdomain, cr.CloudURL, err)
		return
	}
	up := NewCloudUpstream(cr.AppID, target, []byte(cr.EdgeSecret), l.waker, l.freeze)
	if cr.WakeTimeout > 0 {
		up.WakeTimeout = time.Duration(cr.WakeTimeout) * time.Second
	}
	l.store.SetCloud(cr.Subdomain, up)
}

func (l *RouteLoader) reloadAll(ctx context.Context) error {
	routes, err := l.db.LoadCloudRoutes(ctx)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(routes))
	for _, cr := range routes {
		l.apply(cr)
		seen[cr.Subdomain] = true
	}
	// Drop any in-memory routes that no longer exist in the DB.
	var stale []string
	l.store.Each(func(sub string, _ *CloudUpstream) {
		if !seen[sub] {
			stale = append(stale, sub)
		}
	})
	for _, sub := range stale {
		l.store.Delete(sub)
	}

	// Mirror the authoritative set locally. Only ever written after a SUCCESSFUL
	// full read — caching a partial result would let a future boot serve a
	// confidently incomplete route table.
	if err := l.cache.Save(routes); err != nil {
		logger.Warn("route cache save failed: %v (degrade mode will use the previous copy)", err)
	}

	logger.Info("route loader: %d cloud route(s) active", len(routes))
	return nil
}

func (l *RouteLoader) reloadOne(ctx context.Context, subdomain string) {
	cr, ok, err := l.db.GetCloudRoute(ctx, subdomain)
	if err != nil {
		logger.Warn("route reload %s: %v", subdomain, err)
		return
	}
	if !ok {
		l.store.Delete(subdomain)
		return
	}
	l.apply(cr)
}

func (l *RouteLoader) listenLoop(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := l.db.ListenRoutes(ctx, func(sub string) { l.reloadOne(ctx, sub) })
		if ctx.Err() != nil {
			return
		}
		logger.Warn("route LISTEN dropped: %v (reconnecting)", err)
		_ = l.reloadAll(ctx) // catch changes missed while disconnected
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (l *RouteLoader) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = l.reloadAll(ctx)
		}
	}
}
