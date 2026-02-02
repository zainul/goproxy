package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	TrafficSuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "proxy_traffic_success_total",
			Help: "Total successful traffic requests",
		},
		[]string{"upstream", "endpoint"},
	)

	TrafficBlocked = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "proxy_traffic_blocked_total",
			Help: "Total blocked traffic requests",
		},
		[]string{"upstream", "endpoint", "reason"},
	)

	CircuitState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "proxy_circuit_breaker_state",
			Help: "Circuit breaker state (0=closed, 1=half-open, 2=open)",
		},
		[]string{"upstream"},
	)

	RateLimitReached = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "proxy_rate_limit_reached_total",
			Help: "Total times rate limit was reached",
		},
		[]string{"upstream", "endpoint"},
	)
)