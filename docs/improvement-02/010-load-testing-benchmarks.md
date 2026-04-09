# Task 010: Load Testing & Benchmark Suite

**Priority**: P0 (Critical — validates everything)  
**Assigned To**: Both Engineers (joint)  
**Estimated Effort**: 4-5 hours  
**Dependencies**: All previous tasks (001-009) completed  

**Files to Create**:
- `internal/usecase/benchmark_test.go`
- `scripts/loadtest.sh`
- `scripts/loadtest_config.json`

**Files to Modify**:
- `config.json` (final production-ready config)

---

## Problem Statement

The current test suite only verifies correctness, not performance. To validate 10K RPS capability, we need:
1. **Go benchmarks** that measure per-component throughput
2. **Integration load test** that measures end-to-end performance
3. **Documented baseline** to detect regressions

---

## Solution

### Part A: Go Benchmark Tests

**File**: `internal/usecase/benchmark_test.go` (CREATE NEW FILE)

```go
package usecase

import (
    "context"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "goproxy/internal/entity"
    "goproxy/internal/repository"
    "goproxy/pkg/utils"
)

// BenchmarkForwardRequest_NoSingleflight measures raw proxy throughput
func BenchmarkForwardRequest_NoSingleflight(b *testing.B) {
    // Setup a fast backend
    backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status":"ok"}`))
    }))
    defer backend.Close()

    cbManager := NewCircuitBreakerManager()
    cbManager.AddBreaker(backend.URL, utils.CircuitBreakerConfig{
        FailureThreshold: 5,
        SuccessThreshold: 3,
        Timeout:          "10s",
        CounterType:      "ringbuffer",
        WindowSize:       10,
    })

    memRepo := repository.NewMemoryRateLimiterRepository()
    healthChecker := NewHealthChecker(30 * time.Second)
    rlManager := NewRateLimiterManager(memRepo, cbManager, healthChecker)
    rlManager.AddLimiter(backend.URL, utils.RateLimiterConfig{
        Type:   "sliding_window",
        Limit:  1000000, // Very high to not bottleneck
        Window: 60,
    })

    backends := []*entity.Backend{
        {URL: backend.URL, IsHealthy: true, IsReady: true, SuccessRate: 1.0},
    }
    lb := entity.NewLoadBalancer(backends)
    proxy := NewHTTPProxy(cbManager, rlManager, lb, false, 30*time.Second)

    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            req := httptest.NewRequest("POST", backend.URL+"/test", nil)
            w := httptest.NewRecorder()
            proxy.ForwardRequest(w, req, "/test")
        }
    })
}

// BenchmarkForwardRequest_WithSingleflight measures singleflight dedup
func BenchmarkForwardRequest_WithSingleflight(b *testing.B) {
    backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        time.Sleep(1 * time.Millisecond) // Simulate backend latency
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status":"ok"}`))
    }))
    defer backend.Close()

    cbManager := NewCircuitBreakerManager()
    cbManager.AddBreaker(backend.URL, utils.CircuitBreakerConfig{
        FailureThreshold: 5,
        SuccessThreshold: 3,
        Timeout:          "10s",
        CounterType:      "ringbuffer",
        WindowSize:       10,
    })

    memRepo := repository.NewMemoryRateLimiterRepository()
    healthChecker := NewHealthChecker(30 * time.Second)
    rlManager := NewRateLimiterManager(memRepo, cbManager, healthChecker)
    rlManager.AddLimiter(backend.URL, utils.RateLimiterConfig{
        Type:   "sliding_window",
        Limit:  1000000,
        Window: 60,
    })

    backends := []*entity.Backend{
        {URL: backend.URL, IsHealthy: true, IsReady: true, SuccessRate: 1.0},
    }
    lb := entity.NewLoadBalancer(backends)
    proxy := NewHTTPProxy(cbManager, rlManager, lb, true, 30*time.Second)

    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            req := httptest.NewRequest("GET", backend.URL+"/test", nil)
            w := httptest.NewRecorder()
            proxy.ForwardRequest(w, req, "/test")
        }
    })
}

// BenchmarkRateLimiter_Memory measures in-memory rate limiter throughput
func BenchmarkRateLimiter_Memory(b *testing.B) {
    repo := repository.NewMemoryRateLimiterRepository()
    ctx := context.Background()

    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            repo.Allow(ctx, "bench-key", 1000000, time.Minute)
        }
    })
}

// BenchmarkCircuitBreaker_CanExecute measures circuit breaker check throughput
func BenchmarkCircuitBreaker_CanExecute(b *testing.B) {
    cbManager := NewCircuitBreakerManager()
    cbManager.AddBreaker("http://bench.com", utils.CircuitBreakerConfig{
        FailureThreshold: 5,
        SuccessThreshold: 3,
        Timeout:          "10s",
        CounterType:      "ringbuffer",
        WindowSize:       10,
    })

    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            cbManager.CanExecute("http://bench.com")
        }
    })
}

// BenchmarkLoadBalancer_NextHealthyBackend measures LB selection throughput
func BenchmarkLoadBalancer_NextHealthyBackend(b *testing.B) {
    backends := []*entity.Backend{
        {URL: "http://a.com", IsHealthy: true, IsReady: true, Weight: 1},
        {URL: "http://b.com", IsHealthy: true, IsReady: true, Weight: 1},
        {URL: "http://c.com", IsHealthy: true, IsReady: true, Weight: 1},
    }
    lb := entity.NewLoadBalancer(backends)

    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            lb.NextHealthyBackend()
        }
    })
}
```

### Part B: Load Test Script

**File**: `scripts/loadtest.sh` (CREATE NEW FILE)

```bash
#!/bin/bash
# GoProxy Load Test Script
# Requires: hey (https://github.com/rakyll/hey)
#
# Install: go install github.com/rakyll/hey@latest
#
# Usage: ./scripts/loadtest.sh [target_rps] [duration_seconds]

set -e

TARGET_RPS=${1:-10000}
DURATION=${2:-30}
CONCURRENCY=$((TARGET_RPS / 10))  # ~100ms per request → 10 req/s per worker
PROXY_URL=${PROXY_URL:-"http://localhost:8080"}

echo "========================================"
echo "  GoProxy Load Test"
echo "========================================"
echo "Target RPS:    ${TARGET_RPS}"
echo "Duration:      ${DURATION}s"
echo "Concurrency:   ${CONCURRENCY}"
echo "Proxy URL:     ${PROXY_URL}"
echo "========================================"

# Check if hey is installed
if ! command -v hey &> /dev/null; then
    echo "Error: 'hey' is not installed."
    echo "Install with: go install github.com/rakyll/hey@latest"
    exit 1
fi

# Check if proxy is running
if ! curl -s "${PROXY_URL}/metrics" > /dev/null 2>&1; then
    echo "Error: Proxy is not running at ${PROXY_URL}"
    echo "Start it with: go run ./cmd/main.go"
    exit 1
fi

echo ""
echo "--- Phase 1: Warmup (5s, 100 RPS) ---"
hey -z 5s -c 10 -q 10 "${PROXY_URL}/" > /dev/null 2>&1
echo "Warmup complete."

echo ""
echo "--- Phase 2: Ramp (10s, ${TARGET_RPS} RPS) ---"
hey -z 10s -c ${CONCURRENCY} -q $((TARGET_RPS / CONCURRENCY)) "${PROXY_URL}/" 2>&1 | tail -20

echo ""
echo "--- Phase 3: Sustained Load (${DURATION}s, ${TARGET_RPS} RPS) ---"
hey -z ${DURATION}s -c ${CONCURRENCY} -q $((TARGET_RPS / CONCURRENCY)) "${PROXY_URL}/" 2>&1

echo ""
echo "--- Prometheus Metrics Snapshot ---"
echo ""
curl -s "${PROXY_URL}/metrics" | grep -E "^proxy_" | head -30

echo ""
echo "========================================"
echo "  Load Test Complete"
echo "========================================"
```

### Part C: Production-Ready Config

**File**: `scripts/loadtest_config.json` (CREATE NEW FILE)

This is the reference configuration optimized for 10K RPS:

```json
{
  "listen_addr": ":8080",
  "enable_singleflight": true,
  "health_check_interval": "10s",
  "shutdown_timeout": "30s",
  "rate_limiter_storage": "memory",
  "load_balancer_strategy": "least_connections",
  "server": {
    "read_timeout": "30s",
    "write_timeout": "60s",
    "read_header_timeout": "5s",
    "idle_timeout": "120s",
    "max_header_bytes": 65536
  },
  "transport": {
    "max_idle_conns": 1000,
    "max_idle_conns_per_host": 200,
    "max_conns_per_host": 500,
    "idle_conn_timeout": "90s",
    "tls_handshake_timeout": "5s",
    "response_header_timeout": "10s",
    "expect_continue_timeout": "1s",
    "disable_keep_alives": false,
    "disable_compression": false,
    "write_buffer_size": 32768,
    "read_buffer_size": 32768
  },
  "redis": {
    "addr": "localhost:6379",
    "password": "",
    "db": 0,
    "pool_size": 200,
    "min_idle_conns": 20,
    "max_retries": 3,
    "dial_timeout": "5s",
    "read_timeout": "2s",
    "write_timeout": "2s",
    "pool_timeout": "3s",
    "conn_max_lifetime": "30m",
    "conn_max_idle_time": "5m"
  },
  "backends": [
    {
      "url": "http://httpbin.org",
      "weight": 1,
      "timeout": "15s",
      "circuit_breaker": {
        "failure_threshold": 5,
        "success_threshold": 3,
        "timeout": "10s",
        "counter_type": "ringbuffer",
        "window_size": 100
      },
      "rate_limiter": {
        "type": "sliding_window",
        "limit": 10000,
        "window": 1,
        "dynamic": true,
        "damage_level": 0.5,
        "catastrophic_level": 0.8,
        "healthy_increment": 0.05,
        "unhealthy_decrement": 0.1,
        "priority": 1,
        "health_adjustment_factor": 0.5,
        "readiness_adjustment_factor": 0.5,
        "success_rate_threshold": 0.8,
        "success_rate_adjustment_factor": 0.5
      },
      "endpoints": [],
      "health_check_endpoint": "/health",
      "readiness_endpoint": "/ready",
      "statistics_endpoint": "/stats"
    }
  ]
}
```

---

## Running the Benchmarks

```bash
# Run all benchmarks
go test -bench=. -benchmem -count=3 ./internal/usecase/

# Run with CPU profiling
go test -bench=BenchmarkForwardRequest -cpuprofile=cpu.prof ./internal/usecase/
go tool pprof cpu.prof

# Run with memory profiling
go test -bench=BenchmarkForwardRequest -memprofile=mem.prof ./internal/usecase/
go tool pprof mem.prof

# Run load test
chmod +x scripts/loadtest.sh
./scripts/loadtest.sh 10000 30
```

---

## Expected Benchmark Targets

| Benchmark | Target ops/sec | Target allocs/op |
|-----------|---------------|-----------------|
| `BenchmarkForwardRequest_NoSingleflight` | > 15,000 | < 20 |
| `BenchmarkForwardRequest_WithSingleflight` | > 20,000 | < 15 |
| `BenchmarkRateLimiter_Memory` | > 1,000,000 | < 5 |
| `BenchmarkCircuitBreaker_CanExecute` | > 5,000,000 | 0 |
| `BenchmarkLoadBalancer_NextHealthyBackend` | > 10,000,000 | 0 |

---

## Verification Checklist

- [ ] All benchmark tests compile and run
- [ ] Benchmark results meet target thresholds
- [ ] Load test script runs against local proxy
- [ ] Production config file created with all tuning parameters
- [ ] No race conditions: `go test -race -bench=. ./...`
- [ ] Benchmark output saved for baseline comparison

---

## Success Criteria

1. `BenchmarkForwardRequest_NoSingleflight` sustains > 15K ops/sec
2. Load test with `hey` sustains 10K RPS for 30 seconds with < 1% error rate
3. p99 latency < 200ms under 10K RPS sustained load
4. Memory usage remains stable (no growth) during 60-second sustained test
5. All component benchmarks exceed their individual targets
