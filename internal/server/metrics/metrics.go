package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	ActiveSessions = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "relayd_active_sessions",
		Help: "Number of currently connected clients",
	})

	ActiveTunnels = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "relayd_active_tunnels",
		Help: "Number of currently active tunnels",
	})

	ConnectionsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "relayd_connections_total",
		Help: "Total number of client connections",
	})

	AuthFailuresTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "relayd_auth_failures_total",
		Help: "Total number of authentication failures",
	})

	RateLimitHitsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "relayd_rate_limit_hits_total",
		Help: "Total number of rate limit hits",
	})

	BytesProxiedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "relayd_bytes_proxied_total",
		Help: "Total bytes proxied through tunnels",
	})
)

func init() {
	prometheus.MustRegister(
		ActiveSessions,
		ActiveTunnels,
		ConnectionsTotal,
		AuthFailuresTotal,
		RateLimitHitsTotal,
		BytesProxiedTotal,
	)
}
