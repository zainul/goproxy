# GoProxy: Resilient Reverse Proxy

GoProxy is a production-grade reverse proxy written in Go, designed to handle cascading failures from unstable backend services. It incorporates circuit breaker patterns, rate limiting, adaptive load balancing, panic recovery, and graceful shutdown to ensure high availability and reliability.

## Features

- **Circuit Breaker**: Implements finite state machine with CLOSED, OPEN, and HALF-OPEN states using atomic operations for thread-safe concurrent access
  - RingBuffer (fixed-size circular buffer) counter implementation for low-latency failure tracking
  - SlidingWindow time-based counter with efficient GC-friendly cleanup
  - Configurable thresholds: failure_threshold, success_threshold, timeout duration

- **Rate Limiting**: Protects backends with Sliding Window and Token Bucket algorithms supporting both in-memory (optimized for single-instance) and Redis storage backends
  - Dynamic rate limiting based on upstream health status
  - Endpoint-specific limits override backend-level configuration
  - Health-aware adjustments: automatic scaling up/down based on backend health
  - Atomic operations ensure thread-safe concurrent adjustments

- **Health Checking**: Automatic backend health monitoring with configurable intervals
  - Health endpoint monitoring (`/health`)
  - Readiness endpoint checking (`/ready`)
  - Success rate tracking via statistics endpoint (`/stats`)
  - Real-time status updates (Healthy/Unhealthy, Ready/NotReady)
  - Background worker goroutine for efficient polling

- **Load Balancing**: Round-robin backend selection with health-aware routing
  - Prefects healthy and ready backends first
  - Falls back to unhealthy backends when none are available
  - Health-aware routing adjusts traffic distribution automatically

- **Header Sanitization**: Filters request headers using allowlist/blocklist to prevent header injection attacks
  - Blocks dangerous headers: Connection, Keep-Alive, Proxy-Authenticate, etc.
  - Allows safe headers by default or explicit allowlist configuration

- **Metrics**: Prometheus-compatible metrics exposed at `/metrics` endpoint with labels for traffic success/blocked, circuit breaker states, and rate limit events grouped by upstream and endpoint

- **Panic Recovery**: Comprehensive panic handling with stack trace logging to ensure service resilience
  - All HTTP handlers wrapped with panic recovery middleware
  - Automatic error response on panic
  - Stack trace logged for debugging

- **Graceful Shutdown**: Handles SIGINT/SIGTERM signals for clean termination
  - Configurable shutdown timeout (default 30s)
  - Allows ongoing requests to complete before shutdown
  - Cancels background workers and closes connections

- **Singleflight**: Deduplicates identical GET requests to reduce backend load
  - Reduces duplicate processing of the same request

- **Transport Tuning**: Optimized HTTP transport configuration for high-throughput deployments
  - Connection pooling: max_idle_conns, max_idle_conns_per_host, max_conns_per_host
  - Connection timeouts: idle_conn_timeout, tls_handshake_timeout, response_header_timeout
  - Buffer optimization: write_buffer_size, read_buffer_size
  - Compression and keep-alive configuration

- **Server Tuning**: Optimized HTTP server configuration for proxy workloads
  - Read/write headers timeout: read_timeout (30s), write_timeout (60s)
  - Max header bytes limit (default 64KB)
  - Idle connection timeout (120s default)

- **Configuration**: Flexible configuration via JSON or YAML files with comprehensive validation
  - Supports memory and Redis rate limiter storage backends
  - Automatic defaults optimized for proxy workload
  - Schema validation ensures correctness

- **Clean Architecture**: Organized into internal packages (entity, repository, usecase) for maintainability
  - Domain layer: entity package with models
  - Data access: repository package with implementations
  - Business logic: usecase package with interfaces

- **Testing**: Includes mocking for unit tests and benchmarks for performance testing. Supported storage types (memory or Redis)
  - Memory storage optimized for single-instance deployments (10K+ RPS)
  - Redis support for distributed deployments (in development)

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
│  │     with Health-Aware Routing │  │
│  └───────────────┬───────────────┘  │
│                  │                  │
│  ┌───────────────▼───────────────┐  │
│  │    Singleflight (GET only)    │  │
│  └───────────────┬───────────────┘  │
│                  │                  │
│  ┌───────────────▼───────────────┐  │
│  │      HTTP Forwarding          │  │
│  │    with Header Sanitization   │  │
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
| `entity` | Domain | Data models (CircuitBreaker, Backend, LoadBalancer) with atomic operations for concurrency safety |
| `repository` | Data | Rate limiting implementations: MemoryRateLimiterRepository and Redis repository interface |
| `usecase` | Business | Proxy logic, health checking, rate limiting management, circuit breaker orchestration |
| `pkg/*` | Shared | Constants (header blocklist/allowlist), errors, metrics, middleware utilities |

## Deployment Modes

GoProxy supports two deployment modes via the `rate_limiter_storage` configuration option:

### Single-Instance Mode (Memory Storage) - Default

- **Use Case**: Deployments running a single GoProxy instance
- **Storage**: In-memory rate limiter repository
- **Optimization**: Optimized for 10K+ requests per second with zero Redis round-trips
- **Benefits**: Lowest latency, no external dependency required
- **Configuration**: Set `rate_limiter_storage: "memory"` or omit (defaults to memory)

### Cluster Mode (Redis Storage) - Coming Soon

- **Use Case**: Multiple GoProxy instances in a cluster/load-balanced deployment
- **Storage**: Redis-based rate limiter repository
- **Optimization**: Shared state across instances for consistent rate limiting
- **Benefits**: Cross-instance traffic control, persistent limits
- **Configuration**: Set `rate_limiter_storage: "redis"` and provide Redis connection details

## Configuration

Create a `config.json` or `config.yaml` file in the project root directory.

### Quick Start Example (Single Instance)

```json
{
  "listen_addr": ":8080",
  "health_check_interval": "30s",
  "backends": [
    {
      "url": "http://backend1:8080",
      "circuit_breaker": {
        "failure_threshold": 5,
        "success_threshold": 3,
        "timeout": "10s",
        "counter_type": "ringbuffer",
        "window_size": 10,

</string>