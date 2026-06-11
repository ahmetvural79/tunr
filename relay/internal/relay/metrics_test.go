package relay

import "testing"

func TestRecordRequestAndUsage(t *testing.T) {
	r := &Registry{
		tunnels:     map[string]*TunnelEntry{},
		bySubdomain: map[string]*TunnelEntry{},
		metrics:     map[string]*userMetric{},
	}

	// No activity yet → zero usage.
	if reqs, bytes := r.UserUsage("u1"); reqs != 0 || bytes != 0 {
		t.Fatalf("expected zero usage, got reqs=%d bytes=%d", reqs, bytes)
	}

	r.RecordRequest("u1", 100)
	r.RecordRequest("u1", 250)
	r.RecordRequest("u2", 50)

	if reqs, bytes := r.UserUsage("u1"); reqs != 2 || bytes != 350 {
		t.Fatalf("u1: expected reqs=2 bytes=350, got reqs=%d bytes=%d", reqs, bytes)
	}
	if reqs, bytes := r.UserUsage("u2"); reqs != 1 || bytes != 50 {
		t.Fatalf("u2: expected reqs=1 bytes=50, got reqs=%d bytes=%d", reqs, bytes)
	}

	// Empty userID is ignored (anonymous requests don't meter).
	r.RecordRequest("", 999)
	if reqs, _ := r.UserUsage(""); reqs != 0 {
		t.Fatalf("expected empty userID to be ignored, got reqs=%d", reqs)
	}
}

func TestUserUsageDailyReset(t *testing.T) {
	r := &Registry{metrics: map[string]*userMetric{}}
	r.RecordRequest("u1", 10)

	// Simulate yesterday's bucket so the day-rollover path resets it.
	r.metricsMu.Lock()
	r.metrics["u1"].day = "2000-01-01"
	r.metricsMu.Unlock()

	if reqs, bytes := r.UserUsage("u1"); reqs != 0 || bytes != 0 {
		t.Fatalf("expected stale day to reset to zero, got reqs=%d bytes=%d", reqs, bytes)
	}
}
