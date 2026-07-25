package relay

// route_cache.go — on-disk route cache (Çok-node planı, Faz A).
//
// The design rule this implements:
//
//	*Kontrol düzlemi arızası, veri düzlemini durdurmamalı.*
//	A control-plane failure must not stop the data plane.
//
// Today it does. Routes live only in Postgres; if the relay restarts while
// Postgres is down, `reloadAll` fails, the RouteStore is empty, and every
// deployed app returns 404 — even though the containers are running perfectly
// and the relay can reach them. A database that has nothing to do with serving
// traffic becomes a hard dependency for serving traffic.
//
// The fix is a plain local mirror of the routes table. On every successful full
// reload the store is written to disk; on startup, if the DB is unreachable, the
// relay boots from that file and serves normally. New deploys and route changes
// stall until Postgres returns — which is correct, that IS control-plane work —
// but existing apps never notice.
//
// This matters on one node and is survival on several: with N relays, a shared
// Postgres becomes a cluster-wide single point of failure otherwise.
//
// Security note: the cache contains each route's edge HMAC secret, because a
// route is useless without one. The file is written 0600 into a directory the
// relay owns. It is not a new exposure — the same secret already sits in
// Postgres and in the relay's memory — but it must never be written somewhere
// world-readable.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ahmetvural79/tunr/relay/internal/db"
	"github.com/ahmetvural79/tunr/relay/internal/logger"
)

// routeCacheVersion guards against reading a file written by an older layout.
const routeCacheVersion = 1

type cachedRoutes struct {
	Version   int             `json:"version"`
	WrittenAt time.Time       `json:"written_at"`
	Routes    []db.CloudRoute `json:"routes"`
}

// RouteCache persists the cloud route table locally.
//
// A zero Path disables it entirely (every method becomes a no-op), which is the
// right behaviour for tests and for in-memory mode.
type RouteCache struct {
	Path string
}

// NewRouteCache returns a cache writing to path. Empty path = disabled.
func NewRouteCache(path string) *RouteCache { return &RouteCache{Path: path} }

// Enabled reports whether a path was configured.
func (c *RouteCache) Enabled() bool { return c != nil && c.Path != "" }

// Save atomically writes the route set.
//
// Write-to-temp-then-rename, because a torn cache file is worse than none: the
// relay would boot, parse half a route table, and serve a confidently wrong
// subset of apps. Rename on the same filesystem is atomic, so a crash mid-write
// leaves the previous good copy intact.
func (c *RouteCache) Save(routes []db.CloudRoute) error {
	if !c.Enabled() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o700); err != nil {
		return fmt.Errorf("route cache dir: %w", err)
	}

	payload, err := json.Marshal(cachedRoutes{
		Version:   routeCacheVersion,
		WrittenAt: time.Now().UTC(),
		Routes:    routes,
	})
	if err != nil {
		return fmt.Errorf("route cache marshal: %w", err)
	}

	tmp := c.Path + ".tmp"
	// 0600: the payload carries per-route edge HMAC secrets.
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return fmt.Errorf("route cache write: %w", err)
	}
	if err := os.Rename(tmp, c.Path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("route cache rename: %w", err)
	}
	return nil
}

// Load reads the cached route set. A missing file is not an error — it just
// means this relay has never successfully talked to Postgres.
func (c *RouteCache) Load() ([]db.CloudRoute, time.Time, error) {
	if !c.Enabled() {
		return nil, time.Time{}, nil
	}
	b, err := os.ReadFile(c.Path)
	if os.IsNotExist(err) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("route cache read: %w", err)
	}

	var cached cachedRoutes
	if err := json.Unmarshal(b, &cached); err != nil {
		return nil, time.Time{}, fmt.Errorf("route cache parse: %w", err)
	}
	if cached.Version != routeCacheVersion {
		return nil, time.Time{}, fmt.Errorf("route cache version %d (want %d) — ignoring",
			cached.Version, routeCacheVersion)
	}
	return cached.Routes, cached.WrittenAt, nil
}

// LoadInto applies the cached routes to a store via apply, returning how many
// were restored. Used at boot when Postgres is unreachable.
func (c *RouteCache) LoadInto(apply func(db.CloudRoute)) int {
	routes, writtenAt, err := c.Load()
	if err != nil {
		logger.Warn("route cache unusable: %v", err)
		return 0
	}
	if len(routes) == 0 {
		return 0
	}
	for _, r := range routes {
		apply(r)
	}
	// Age matters: a cache from three days ago may be missing apps deployed
	// since, so an operator reading the log knows how much to trust it.
	logger.Warn("route cache: serving %d route(s) from local cache written %s ago — "+
		"existing apps work, new deploys and route changes are BLOCKED until Postgres returns",
		len(routes), time.Since(writtenAt).Round(time.Second))
	return len(routes)
}
