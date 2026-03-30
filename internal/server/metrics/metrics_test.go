package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetrics_ActiveSessions(t *testing.T) {
	ActiveSessions.Set(0)
	ActiveSessions.Inc()
	ActiveSessions.Inc()
	if v := testutil.ToFloat64(ActiveSessions); v != 2 {
		t.Errorf("expected 2, got %v", v)
	}
	ActiveSessions.Dec()
	if v := testutil.ToFloat64(ActiveSessions); v != 1 {
		t.Errorf("expected 1, got %v", v)
	}
}

func TestMetrics_ConnectionsTotal(t *testing.T) {
	before := testutil.ToFloat64(ConnectionsTotal)
	ConnectionsTotal.Add(5)
	after := testutil.ToFloat64(ConnectionsTotal)
	if after-before != 5 {
		t.Errorf("expected +5, got %v", after-before)
	}
}

func TestMetrics_AuthFailures(t *testing.T) {
	before := testutil.ToFloat64(AuthFailuresTotal)
	AuthFailuresTotal.Inc()
	after := testutil.ToFloat64(AuthFailuresTotal)
	if after-before != 1 {
		t.Errorf("expected +1, got %v", after-before)
	}
}

func TestMetrics_RateLimitHits(t *testing.T) {
	before := testutil.ToFloat64(RateLimitHitsTotal)
	RateLimitHitsTotal.Inc()
	after := testutil.ToFloat64(RateLimitHitsTotal)
	if after-before != 1 {
		t.Errorf("expected +1, got %v", after-before)
	}
}

func TestMetrics_BytesProxied(t *testing.T) {
	before := testutil.ToFloat64(BytesProxiedTotal)
	BytesProxiedTotal.Add(1024)
	after := testutil.ToFloat64(BytesProxiedTotal)
	if after-before != 1024 {
		t.Errorf("expected +1024, got %v", after-before)
	}
}
