# GoProxy: Resilient Reverse Proxy

GoProxy is a resilient reverse proxy written in Go, designed to handle cascading failures from unstable backend services. It incorporates circuit breaker patterns, rate limiting, and singleflight to prevent failures from propagating and manage load, ensuring high availability and reliability.

## Features

- **Circuit Breaker**: Implements a finite state machine with CLOSED, OPEN, and HALF-OPEN states to manage backend health.
- **Rate Limiting**: Protects backends with Sliding Window and Token Bucket algorithms using Redis, with dynamic adjustment based on health and endpoint priority.
- **Health Checking**: Automatic backend health monitoring with configurable intervals
  - Health endpoint monitoring (`/health`)
  - Readiness endpoint checking (`/ready`)
  - Success rate tracking via statistics endpoint
  - Dynamic status updates (Healthy/Unhealthy, Ready/NotReady)
- **Load Balancing**: Round-robin backend selection with health-aware routing.
- **Header Sanitization**: Filters request headers using allowlist/blocklist to prevent header injection.
- **Metrics**: Prometheus-compatible metrics exposed at `/metrics` endpoint, including traffic success/blocked, circuit breaker states, and rate limit hits, grouped by upstream and endpoint.
- **Panic Recovery**: Comprehensive panic handling with stack trace logging to ensure service resilience.
- **Graceful Shutdown**: Handles SIGINT/SIGTERM signals for clean termination, allowing ongoing requests to complete.
- **Counter Algorithms**: Supports RingBuffer (fixed-size circular buffer) and Sliding Window (time-based window) for tracking success/failure metrics.
- **Concurrency Safety**: Uses atomic operations and mutexes for thread-safe counter updates in dynamic rate limiting.
- **Singleflight**: Deduplicates identical GET requests to reduce backend load.
- **Configuration**: Flexible configuration via JSON or YAML files.
- **Clean Architecture**: Organized into internal packages (entity, repository, usecase) for maintainability.
- **Testing**: Includes mocking for unit tests and benchmarks for performance.

## Architecture

```
┌─────────────┐
│   Client    │
└──────┬──────┘
       │
       ▼
┌─────────────────────────────────────┐
│           GoProxy Server            │
│  ┌───────────────────────────────┐  │
│  │     Panic Recovery Middleware │  │
│  └───────────────┬───────────────┘  │
│                  │                  │
│  ┌───────────────▼───────────────┐  │
│  │      Rate Limiter Check       │  │
│  └───────────────┬───────────────┘  │
│                  │                  │
│  ┌───────────────▼───────────────┐  │
│  │    Circuit Breaker Check      │  │
│  └───────────────┬───────────────┘  │
│                  │                  │
│  ┌───────────────▼───────────────┐  │
│  │     Load Balancer (RR)        │  │
│  └───────────────┬───────────────┘  │
│                  │                  │
│  ┌───────────────▼───────────────┐  │
│  │    Singleflight (GET only)    │  │
│  └───────────────┬───────────────┘  │
│                  │                  │
│  ┌───────────────▼───────────────┐  │
│  │      HTTP Forwarding          │  │
│  └───────────────────────────────┘  │
└─────────────────────────────────────┘
       │
       ▼
┌─────────────┐     ┌─────────────┐
│  Backend 1  │     │  Backend 2  │
└─────────────┘     └─────────────┘
```

### Components

| Component | Layer | Responsibility |
|-----------|-------|----------------|
| `entity` | Domain | Data models (CircuitBreaker, Backend, LoadBalancer) |
| `repository` | Data | Redis rate limiting, metrics storage |
| `usecase` | Business | Proxy logic, health checking, rate limiting, circuit breaker management |
| `pkg/*` | Shared | Constants, errors, metrics, middleware, utilities |

## Configuration

Create a `config.json` or `config.yaml` file in the root directory.

### JSON Example

```json
{
  "listen_addr": ":8080",
  "enable_singleflight": true,
  "health_check_interval": "30s",
  "redis": {
    "addr": "localhost:6379",
    "password": "",
    "db": 0
  },
  "backends": [
    {
      "url": "http://backend1:8080",
      "circuit_breaker": {
        "failure_threshold": 5,
        "success_threshold": 3,
        "timeout": "10s",
        "counter_type": "ringbuffer",
        "window_size": 10
      },
      "rate_limiter": {
        "type": "sliding_window",
        "limit": 100,
        "window": 60,
        "dynamic": true,
        "damage_level": 0.5,
        "catastrophic_level": 0.8,
        "healthy_increment": 0.01,
        "unhealthy_decrement": 0.01,
        "priority": 1,
        "health_adjustment_factor": 0.5,
        "readiness_adjustment_factor": 0.5,
        "success_rate_threshold": 0.8,
        "success_rate_adjustment_factor": 0.5
      },
      "endpoints": [
        {
          "path": "/api/high",
          "rate_limiter": {
            "type": "token_bucket",
            "limit": 50,
            "window": 10,
            "dynamic": true,
            "damage_level": 0.3,
            "catastrophic_level": 0.7,
            "healthy_increment": 0.02,
            "unhealthy_decrement": 0.02,
            "priority": 3
          }
        }
      ],
      "health_check_endpoint": "/health",
      "readiness_endpoint": "/ready",
      "statistics_endpoint": "/stats"
    },
    {
      "url": "http://backend2:8080",
      "circuit_breaker": {
        "failure_threshold": 0.5,
        "success_threshold": 0.7,
        "timeout": "5s",
        "counter_type": "sliding_window",
        "window_size": 60
      },
      "rate_limiter": {
        "type": "token_bucket",
        "limit": 50,
        "window": 10
      }
    }
  ]
}
```

### YAML Example

```yaml
listen_addr: ":8080"
enable_singleflight: true
health_check_interval: "30s"
redis:
  addr: "localhost:6379"
  password: ""
  db: 0
backends:
  - url: "http://backend1:8080"
    circuit_breaker:
      failure_threshold: 5
      success_threshold: 3
      timeout: "10s"
      counter_type: "ringbuffer"
      window_size: 10
    rate_limiter:
      type: "sliding_window"
      limit: 100
      window: 60
      dynamic: true
      damage_level: 0.5
      catastrophic_level: 0.8
      healthy_increment: 0.01
      unhealthy_decrement: 0.01
      priority: 1
      health_adjustment_factor: 0.5
      readiness_adjustment_factor: 0.5
      success_rate_threshold: 0.8
      success_rate_adjustment_factor: 0.5
    endpoints:
      - path: "/api/high"
        rate_limiter:
          type: "token_bucket"
          limit: 50
          window: 10
          dynamic: true
          damage_level: 0.3
          catastrophic_level: 0.7
          healthy_increment: 0.02
          unhealthy_decrement: 0.02
          priority: 3
    health_check_endpoint: "/health"
    readiness_endpoint: "/ready"
    statistics_endpoint: "/stats"
  - url: "http://backend2:8080"
    circuit_breaker:
      failure_threshold: 0.5
      success_threshold: 0.7
      timeout: "5s"
      counter_type: "sliding_window"
      window_size: 60
    rate_limiter:
      type: "token_bucket"
      limit: 50
      window: 10
```

### Configuration Options

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `listen_addr` | string | Yes | Address to listen on for proxy requests |
| `enable_singleflight` | bool | No | Enable singleflight to deduplicate GET requests (default false) |
| `health_check_interval` | string | No | Health check interval duration (e.g., "30s", "1m"; default "30s") |
| `rate_limiter.type` | string | Yes | `sliding_window` or `token_bucket` |
| `rate_limiter.limit` | int | Yes | Max requests/tokens |
| `rate_limiter.window` | int | Yes | Window in seconds (sliding) or refill rate per second (token bucket) |
| `rate_limiter.dynamic` | bool | No | Enable dynamic rate limiting based on upstream health |
| `rate_limiter.damage_level` | float | No | Damage threshold (e.g., 0.5) |
| `rate_limiter.catastrophic_level` | float | No | Catastrophic threshold (e.g., 0.8) |
| `rate_limiter.healthy_increment` | float | No | Percentage increment when healthy (e.g., 0.01) |
| `rate_limiter.unhealthy_decrement` | float | No | Percentage decrement when unhealthy (e.g., 0.01) |
| `rate_limiter.priority` | int | No | Priority level (higher = more lenient limits) |
| `rate_limiter.endpoints` | array | No | Endpoint-specific rate limits |
| `circuit_breaker.failure_threshold` | float | Yes | Threshold for failures (integer for ringbuffer, float for sliding window rate) |
| `circuit_breaker.success_threshold` | float | Yes | Threshold for successes in half-open state |
| `circuit_breaker.timeout` | string | Yes | Timeout for open state (e.g., "10s") |
| `circuit_breaker.counter_type` | string | Yes | `ringbuffer` or `sliding_window` |
| `circuit_breaker.window_size` | int | Yes | Size of the window (count for ringbuffer, seconds for sliding window) |
| `backends[].url` | string | Yes | Backend URL |
| `backends[].health_check_endpoint` | string | No | Health check path |
| `backends[].readiness_endpoint` | string | No | Readiness check path |
| `backends[].statistics_endpoint` | string | No | Statistics endpoint path |
| `redis.addr` | string | Yes | Redis address |
| `redis.password` | string | No | Redis password |
| `redis.db` | int | No | Redis database number |

### Dynamic Rate Limiting

Rate limits are automatically adjusted based on backend health:

| Condition | Multiplier | Effect |
|-----------|------------|--------|
| Backend Healthy | 1.5x | Increased capacity |
| Backend Ready | 1.3x | Moderate increase |
| High Success Rate (>50%) | 1.2x | Slight increase |
| Unhealthy Backend | 0.5x | Reduced capacity |

This ensures traffic is throttled when backends are struggling.

## Error Handling

GoProxy uses structured error handling with user-friendly and developer-friendly messages:

| Error Type | HTTP Status | Use Case |
|------------|-------------|----------|
| `NotFound` | 404 | Resource not found |
| `InvalidInput` | 400 | Bad request |
| `Internal` | 500 | Server error |
| `External` | 502 | Backend error |
| `RateLimitExceeded` | 429 | Rate limit hit |
| `CircuitBreakerOpen` | 503 | Circuit breaker tripped |

Errors include both a user message (safe for clients) and a developer message (for logging).

## Running

1. Clone the repository.
2. Create a configuration file as above (e.g., `config.json`).
3. Run: `go run ./cmd/main.go`

Or build and run: `go build -o proxy ./cmd && ./proxy`

## Testing

Run unit tests: `go test ./...`

The codebase uses interfaces for dependency injection, allowing easy mocking with libraries like `testify/mock`. Example tests are structured in `*_test.go` files alongside the code.

### Test Cases Covered
- **Normal Proxy Operation**: Verifies requests are forwarded correctly when backend is healthy.
- **Circuit Breaker Open**: Ensures requests are blocked when circuit is open.
- **Circuit Breaker Half-Open**: Tests transition from half-open to closed on successes.
- **Rate Limiting**: Tests sliding window and token bucket algorithms with mocks.
- **High Traffic Handling**: Simulates 5000 concurrent requests to check stability and performance.
- **Configuration Parsing**: Tests JSON and YAML config parsing with rate limiting.
- **Health Checker**: Tests backend health monitoring, readiness checking, and success rate tracking.
- **Header Sanitization**: Verifies blocked headers are stripped and allowed headers are forwarded.
- **Middleware Panic Recovery**: Tests panic recovery and normal request passthrough.

Run benchmarks: `go test -bench=. ./internal/usecase`

## Resilience Features

- **Panic Recovery**: All HTTP handlers and core functions include panic recovery with detailed stack trace logging to prevent service crashes.
- **Graceful Shutdown**: The server responds to interrupt signals (Ctrl+C, SIGTERM) and allows up to 30 seconds for active requests to complete before shutting down.
- **Atomic Operations**: Dynamic rate limiting uses atomic counters and mutexes for thread-safe adjustments under high concurrency.

## Metrics

The proxy exposes Prometheus metrics at `/metrics` endpoint:

- `proxy_traffic_success_total{upstream, endpoint}`: Successful requests
- `proxy_traffic_blocked_total{upstream, endpoint, reason}`: Blocked requests (rate_limit, circuit_breaker, etc.)
- `proxy_circuit_breaker_state{upstream}`: Circuit breaker state (0=closed, 1=half-open, 2=open)
- `proxy_rate_limit_reached_total{upstream, endpoint}`: Rate limit threshold hits

## Dependencies

- Standard library for core functionality.
- For testing: `github.com/stretchr/testify` for mocks.

Install dependencies: `go mod tidy`
