package relay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ahmetvural79/tunr/relay/internal/db"
)

func sampleRoutes() []db.CloudRoute {
	return []db.CloudRoute{
		{Subdomain: "alpha", AppID: "a_1", CloudURL: "http://172.20.0.5:8080", WakeTimeout: 30, EdgeSecret: "s1"},
		{Subdomain: "beta", AppID: "a_2", CloudURL: "http://172.20.0.6:8080", WakeTimeout: 45, EdgeSecret: "s2"},
	}
}

func TestRouteCacheRoundTrip(t *testing.T) {
	c := NewRouteCache(filepath.Join(t.TempDir(), "routes.json"))
	want := sampleRoutes()

	if err := c.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, writtenAt, err := c.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("loaded %d routes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("route %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if writtenAt.IsZero() {
		t.Error("writtenAt is zero — cache age can't be reported")
	}
}

// The cache holds per-route edge HMAC secrets, so it must not be readable by
// anyone but the relay.
func TestRouteCacheFileIsNotWorldReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "routes.json")
	c := NewRouteCache(path)
	if err := c.Save(sampleRoutes()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("cache file mode = %o, want no group/other access", perm)
	}
}

// A missing cache is the normal first-boot state, not a failure.
func TestRouteCacheMissingFileIsNotAnError(t *testing.T) {
	c := NewRouteCache(filepath.Join(t.TempDir(), "absent.json"))
	got, _, err := c.Load()
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if got != nil {
		t.Fatalf("got %d routes from a missing file", len(got))
	}
}

// A zero path disables the cache; every method must be a safe no-op so tests
// and in-memory mode don't need to special-case it.
func TestRouteCacheDisabled(t *testing.T) {
	c := NewRouteCache("")
	if c.Enabled() {
		t.Fatal("empty path should be disabled")
	}
	if err := c.Save(sampleRoutes()); err != nil {
		t.Fatalf("Save on disabled cache: %v", err)
	}
	if n := c.LoadInto(func(db.CloudRoute) { t.Fatal("apply called on disabled cache") }); n != 0 {
		t.Fatalf("LoadInto returned %d", n)
	}
}

// A nil cache must behave like a disabled one — RouteLoader calls these methods
// unconditionally, and a nil-pointer panic there would take the relay down at
// exactly the moment the degrade path is supposed to save it.
func TestRouteCacheNilIsSafe(t *testing.T) {
	var c *RouteCache
	if c.Enabled() {
		t.Fatal("nil cache should not report enabled")
	}
	if err := c.Save(sampleRoutes()); err != nil {
		t.Fatalf("Save on nil cache: %v", err)
	}
	if n := c.LoadInto(func(db.CloudRoute) { t.Fatal("apply called on nil cache") }); n != 0 {
		t.Fatalf("LoadInto on nil cache returned %d", n)
	}
}

// A cache written by an incompatible layout must be rejected outright rather
// than half-parsed into a wrong route table.
func TestRouteCacheRejectsVersionMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routes.json")
	blob, err := json.Marshal(map[string]any{
		"version": routeCacheVersion + 1,
		"routes":  sampleRoutes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err := NewRouteCache(path).Load(); err == nil {
		t.Fatal("expected a version-mismatch error")
	}
}

// Corrupt JSON must produce an error, never a partial route set.
func TestRouteCacheRejectsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routes.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"routes":[{"subdo`), 0o600); err != nil {
		t.Fatal(err)
	}

	got, _, err := NewRouteCache(path).Load()
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if got != nil {
		t.Fatalf("got %d routes from a corrupt file", len(got))
	}
}

// Save must not leave a .tmp file behind — the atomic rename is what stops a
// crash mid-write from producing a torn cache.
func TestRouteCacheSaveLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routes.json")
	if err := NewRouteCache(path).Save(sampleRoutes()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestRouteCacheLoadIntoAppliesEveryRoute(t *testing.T) {
	c := NewRouteCache(filepath.Join(t.TempDir(), "routes.json"))
	if err := c.Save(sampleRoutes()); err != nil {
		t.Fatal(err)
	}

	seen := map[string]string{}
	n := c.LoadInto(func(r db.CloudRoute) { seen[r.Subdomain] = r.AppID })

	if n != 2 {
		t.Fatalf("LoadInto returned %d, want 2", n)
	}
	if seen["alpha"] != "a_1" || seen["beta"] != "a_2" {
		t.Fatalf("applied routes = %v", seen)
	}
}

// Overwriting must fully replace the previous contents, not merge with them —
// a deleted route reappearing after a restart would resurrect a dead subdomain.
func TestRouteCacheSaveReplaces(t *testing.T) {
	c := NewRouteCache(filepath.Join(t.TempDir(), "routes.json"))
	if err := c.Save(sampleRoutes()); err != nil {
		t.Fatal(err)
	}
	if err := c.Save([]db.CloudRoute{{Subdomain: "only", AppID: "a_9", CloudURL: "http://x:8080"}}); err != nil {
		t.Fatal(err)
	}

	got, _, err := c.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Subdomain != "only" {
		t.Fatalf("got %+v, want just the second write", got)
	}
}
