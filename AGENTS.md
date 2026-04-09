# GoProxy

## Build & Run

- Entry point: `cmd/main.go`
- Run: `go run ./cmd/main.go`
- Build: `go build -o proxy ./cmd`
- Tests: `go test ./...`
- Benchmarks: `go test -bench=. ./internal/usecase`

## Architecture

- `internal/entity/` - CircuitBreaker, LoadBalancer models
- `internal/usecase/` - Proxy, HealthChecker, RateLimiterManager, CircuitBreakerManager
- `internal/repository/` - Redis rate limiter storage
- `pkg/` - Constants, errors, metrics, middleware, utilities

## Key Conventions

- Config file: `config.json` or `config.yaml` in project root
- Prometheus metrics registered in `cmd/main.go`
- Uses `testify/mock` for mocking in tests
- Redis required for rate limiting

## Known Issues

- Build fails due to unused `fmt` import in `cmd/main.go`
