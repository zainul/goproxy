# Task 008: Prometheus Metrics for Performance Observability

**Priority**: P1 (High)  
**Assigned To**: Engineer B  
**Estimated Effort**: 3-4 hours  
**Dependencies**: Task 002 (Response Streaming) should be completed first  

**Files to Modify**:
- `pkg/metrics/metrics.go`
- `internal/usecase/proxy.go`
- `cmd/main.go`

---

## Problem Statement

The current metrics only track success/failure counts:

```go
// pkg/metrics/metrics.go
TrafficSuccess   // Counter — total successful requests
TrafficBlocked   // Counter — total blocked requests
CircuitState     // Gauge — circuit breaker state
RateLimitReached // Counter — rate limit hits
```

To validate that the proxy can sustain 10K RPS, we need:

1. **Request latency histograms** — p50/p95/p99 latency to detect tail latency issues
2. **In-flight request gauge** — detect goroutine pile-up
3. **Response size histogram** — understand memory pressure from large responses
4. **Backend latency per backend** — identify slow backends
5. **Connection pool metrics** — verify connection reuse

Without these, it's impossible to identify bottlenecks during load testing.

---

## Solution

### Step 1: Add New Metrics Definitions

**File**: `pkg/metrics/metrics.go`

Add the following metrics after the existing definitions:

```go
var (
    // ... existing metrics ...

    // Request duration histogram with backend and method labels
    RequestDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "proxy_request_duration_seconds",
            Help:    "Request duration in seconds",
            Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
        },
        []string{"upstream", "endpoint", "method", "status"},
    )

    // Backend response time (time from sending request to receiving first byte)
    BackendLatency = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "proxy_backend_latency_seconds",
            Help:    "Backend response latency in seconds (TTFB)",
            Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
        },
        []string{"upstream"},
    )

    // In-flight requests gauge
    InFlightRequests = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "proxy_in_flight_requests",
            Help: "Number of currently in-flight proxy requests",
        },
    )

    // Response size histogram
    ResponseSize = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "proxy_response_size_bytes",
            Help:    "Response body size in bytes",
            Buckets: []float64{100, 1000, 10000, 100000, 1000000, 10000000},
        },
        []string{"upstream", "endpoint"},
    )

    // Request size histogram
    RequestSize = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "proxy_request_size_bytes",
            Help:    "Request body size in bytes (Content-Length)",
            Buckets: []float64{100, 1000, 10000, 100000, 1000000},
        },
        []string{"upstream", "endpoint"},
    )

    // Singleflight dedup counter
    SingleflightDedup = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "proxy_singleflight_dedup_total",
            Help: "Total number of deduplicated requests via singleflight",
        },
        []string{"upstream", "endpoint"},
    )

    // Connection pool stats (from http.Transport)
    ConnectionPoolActive = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "proxy_connection_pool_active",
            Help: "Number of active connections in the HTTP transport pool",
        },
        []string{"state"},  // "idle", "active"
    )
)
```

### Step 2: Register New Metrics

**File**: `cmd/main.go`

**FIND** (line ~32):
```go
prometheus.MustRegister(metrics.TrafficSuccess, metrics.TrafficBlocked, metrics.CircuitState, metrics.RateLimitReached)
```

**REPLACE** with:
```go
prometheus.MustRegister(
    metrics.TrafficSuccess,
    metrics.TrafficBlocked,
    metrics.CircuitState,
    metrics.RateLimitReached,
    metrics.RequestDuration,
    metrics.BackendLatency,
    metrics.InFlightRequests,
    metrics.ResponseSize,
    metrics.RequestSize,
    metrics.SingleflightDedup,
    metrics.ConnectionPoolActive,
)
```

### Step 3: Instrument ForwardRequest

**File**: `internal/usecase/proxy.go`

Add timing instrumentation at the start and end of `ForwardRequest`:

At the **beginning** of `ForwardRequest`, add:
```go
startTime := time.Now()
metrics.InFlightRequests.Inc()
defer func() {
    metrics.InFlightRequests.Dec()
}()
```

After the request completes (before returning nil at the end of the method), add:
```go
// Record request duration
elapsed := time.Since(startTime).Seconds()
status := "success"
if proxyResp != nil && proxyResp.StatusCode >= 400 {
    status = "error"
}
metrics.RequestDuration.WithLabelValues(backendURL, endpoint, r.Method, status).Observe(elapsed)
```

For the streaming path, record after `doStreamingRequest` returns.

### Step 4: Instrument Backend Latency

**File**: `internal/usecase/proxy.go`

In `doBufferedRequest` (and `doStreamingRequest`), wrap the `httpClient.Do` call:

```go
backendStart := time.Now()
httpResp, err := p.httpClient.Do(req)
backendElapsed := time.Since(backendStart).Seconds()
metrics.BackendLatency.WithLabelValues(backendURL).Observe(backendElapsed)
```

### Step 5: Track Response Size

**File**: `internal/usecase/proxy.go`

In `doBufferedRequest`, after reading the body:
```go
metrics.ResponseSize.WithLabelValues(backendURL, endpoint).Observe(float64(len(body)))
```

In `doStreamingRequest`, use a counting writer wrapper or `httpResp.ContentLength`:
```go
if httpResp.ContentLength > 0 {
    metrics.ResponseSize.WithLabelValues(backendURL, endpoint).Observe(float64(httpResp.ContentLength))
}
```

### Step 6: Track Singleflight Deduplication

**File**: `internal/usecase/proxy.go`

In the singleflight path, the `sf.Do` returns a `shared` boolean:

**FIND**:
```go
result, err, _ := p.sf.Do(key, func() (interface{}, error) {
```

**REPLACE** with:
```go
result, err, shared := p.sf.Do(key, func() (interface{}, error) {
```

After the `sf.Do` call:
```go
if shared {
    metrics.SingleflightDedup.WithLabelValues(backendURL, endpoint).Inc()
}
```

### Step 7: Track Request Size

**File**: `internal/usecase/proxy.go`

At the beginning of `ForwardRequest`:
```go
// Record request size
if r.ContentLength > 0 {
    metrics.RequestSize.WithLabelValues(backendURL, endpoint).Observe(float64(r.ContentLength))
}
```

(Note: `backendURL` is available after backend selection, so place this accordingly.)

---

## Verification Checklist

- [ ] All 7 new metrics defined in `pkg/metrics/metrics.go`
- [ ] All metrics registered in `cmd/main.go`
- [ ] `RequestDuration` recorded for every request path
- [ ] `BackendLatency` recorded for backend HTTP calls
- [ ] `InFlightRequests` incremented/decremented correctly
- [ ] `ResponseSize` recorded for buffered and streaming paths
- [ ] `SingleflightDedup` tracks shared responses
- [ ] All tests pass: `go test ./...`
- [ ] Manual test: Start server, hit `/metrics`, verify all new metrics appear

---

## Verification via /metrics Endpoint

After starting the proxy, send test requests and check:

```bash
curl http://localhost:8080/metrics | grep proxy_

# Expected output includes:
# proxy_request_duration_seconds_bucket{...}
# proxy_backend_latency_seconds_bucket{...}
# proxy_in_flight_requests 0
# proxy_response_size_bytes_bucket{...}
# proxy_singleflight_dedup_total{...}
```

---

## Key Metrics for 10K RPS Validation

| Metric | Target at 10K RPS | Alert Threshold |
|--------|-------------------|-----------------|
| `proxy_request_duration_seconds` p99 | < 100ms | > 500ms |
| `proxy_backend_latency_seconds` p99 | < 50ms | > 200ms |
| `proxy_in_flight_requests` | < 500 | > 2000 |
| `proxy_response_size_bytes` median | varies | > 10MB |
| `proxy_singleflight_dedup_total` rate | > 0 | N/A |
