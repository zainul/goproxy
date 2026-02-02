# GoProxy: Resilient Reverse Proxy

GoProxy is a resilient reverse proxy written in Go, designed to handle cascading failures from unstable backend services. It incorporates circuit breaker patterns, rate limiting, and singleflight to prevent failures from propagating and manage load, ensuring high availability and reliability.

## Features

- **Circuit Breaker**: Implements a finite state machine with CLOSED, OPEN, and HALF-OPEN states to manage backend health.
- **Rate Limiting**: Protects backends with Sliding Window and Token Bucket algorithms using Redis.
- **Counter Algorithms**: Supports RingBuffer (fixed-size circular buffer) and Sliding Window (time-based window) for tracking success/failure metrics.
- **Concurrency Safety**: Uses atomic operations for thread-safe counter updates.
- **Singleflight**: Deduplicates identical GET requests to reduce backend load.
- **Configuration**: Flexible configuration via JSON or YAML files.
- **Clean Architecture**: Organized into internal packages (entity, repository, usecase) for maintainability.
- **Testing**: Includes mocking for unit tests and benchmarks for performance.

## Architecture

The project follows clean architecture principles:

- `cmd/`: Entry point for the application.
- `internal/entity/`: Data models and structs.
- `internal/repository/`: Data access objects (DAOs) for storage.
- `internal/usecase/`: Business logic.
- `pkg/utils/`: General utilities and helpers.

All interactions between packages use interfaces for easy mocking and testing.

## Configuration

Create a `config.json` or `config.yaml` file in the root directory.

### JSON Example

```json
{
  "listen_addr": ":8080",
  "enable_singleflight": true,
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
        "priority": 1
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
      ]
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

- `listen_addr`: Address to listen on for proxy requests.
- `enable_singleflight`: Enable singleflight to deduplicate identical GET requests to reduce backend load (default false). Only applies to GET methods for safety.
- `redis`: Redis configuration for rate limiting.
- `backends`: List of backend services.
  - `rate_limiter`:
    - `type`: "sliding_window" or "token_bucket".
    - `limit`: Max requests/tokens.
    - `window`: Window in seconds for sliding, rate per second for token bucket.
    - `dynamic`: Enable dynamic rate limiting based on upstream health.
    - `damage_level`: Damage threshold (e.g., 0.5).
    - `catastrophic_level`: Catastrophic threshold (e.g., 0.8).
    - `healthy_increment`: Percentage increment when healthy (e.g., 0.01).
    - `unhealthy_decrement`: Percentage decrement when unhealthy (e.g., 0.01).
    - `priority`: Priority level (higher = more lenient limits).
- `endpoints`: List of endpoint-specific configurations with path and rate_limiter.
  - `url`: Backend URL.
  - `circuit_breaker`:
    - `failure_threshold`: Threshold for failures (integer for ringbuffer, float for sliding window rate).
    - `success_threshold`: Threshold for successes in half-open state.
    - `timeout`: Timeout for open state.
    - `counter_type`: "ringbuffer" or "sliding_window".
    - `window_size`: Size of the window (count for ringbuffer, seconds for sliding window).

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

Run benchmarks: `go test -bench=. ./internal/usecase`

### Test Cases Covered
- **Normal Proxy Operation**: Verifies requests are forwarded correctly when backend is healthy.
- **Circuit Breaker Open**: Ensures requests are blocked when circuit is open.
- **Circuit Breaker Half-Open**: Tests transition from half-open to closed on successes.
- **High Traffic Handling**: Simulates 5000 concurrent requests to check stability and performance.
- **Configuration Loading**: Tests JSON and YAML config parsing.

Run benchmarks: `go test -bench=. ./internal/usecase`

## Dependencies

- Standard library for core functionality.
- For testing: `github.com/stretchr/testify` for mocks.

Install dependencies: `go mod tidy`