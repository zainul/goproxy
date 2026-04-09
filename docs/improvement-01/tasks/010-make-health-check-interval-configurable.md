# Task 010: Make Health Check Interval Configurable

**Priority**: P2 (Medium)  
**Estimated Complexity**: Low  
**Files to Modify**: 
- `pkg/utils/config.go`
- `cmd/main.go`

---

## Problem Statement

The health check interval is hardcoded to 30 seconds in `cmd/main.go`:

```go
healthChecker.Start(30 * time.Second)
```

Users cannot adjust this without recompiling.

---

## Solution

Add `health_check_interval` to the configuration file.

---

## Step-by-Step Instructions

### Step 1: Add Config Field

**File**: `pkg/utils/config.go`

**FIND** the `Config` struct:

```go
type Config struct {
    RateLimiter    RateLimiterConfig    `json:"rate_limiter" yaml:"rate_limiter"`
    CircuitBreaker CircuitBreakerConfig `json:"circuit_breaker" yaml:"circuit_breaker"`
    Backends       []BackendConfig      `json:"backends" yaml:"backends"`
    Redis          RedisConfig          `json:"redis" yaml:"redis"`
}
```

**REPLACE** with:

```go
type Config struct {
    RateLimiter        RateLimiterConfig    `json:"rate_limiter" yaml:"rate_limiter"`
    CircuitBreaker     CircuitBreakerConfig `json:"circuit_breaker" yaml:"circuit_breaker"`
    Backends           []BackendConfig      `json:"backends" yaml:"backends"`
    Redis              RedisConfig          `json:"redis" yaml:"redis"`
    HealthCheckInterval string               `json:"health_check_interval" yaml:"health_check_interval"`
}
```

---

### Step 2: Add Default Value in LoadConfig

**File**: `pkg/utils/config.go`

**FIND** the end of `LoadConfig()` (before validation):

**ADD** this code:

```go
// Set default health check interval if not specified
if config.HealthCheckInterval == "" {
    config.HealthCheckInterval = "30s"
}
```

---

### Step 3: Update main.go

**File**: `cmd/main.go`

**FIND**:

```go
healthChecker.Start(30 * time.Second)
```

**REPLACE** with:

```go
healthCheckInterval, err := time.ParseDuration(config.HealthCheckInterval)
if err != nil {
    log.Fatalf("Invalid health_check_interval: %v", err)
}
log.Printf("Starting health checker with interval: %s", healthCheckInterval)
healthChecker.Start(healthCheckInterval)
```

---

### Step 4: Update config.json

**File**: `config.json`

**ADD** this field to the root of the config:

```json
{
  "health_check_interval": "30s",
  "rate_limiter": { ... },
  ...
}
```

---

### Step 5: Add Validation

**File**: `pkg/utils/config.go`

**ADD** to `ValidateConfig()`:

```go
_, err = time.ParseDuration(config.HealthCheckInterval)
if err != nil {
    errors = append(errors, fmt.Sprintf("health_check_interval is invalid: %v", err))
}
```

---

### Step 6: Run Tests

```bash
# Run config tests
go test ./pkg/utils -v

# Run all tests
go test ./...

# Build and verify
go build ./...
```

---

## Verification Checklist

- [ ] `HealthCheckInterval` field added to Config struct
- [ ] Default value set to "30s" in LoadConfig
- [ ] main.go parses and uses config value
- [ ] config.json updated with new field
- [ ] Validation added for duration format
- [ ] All tests pass: `go test ./...`

---

## Success Criteria

1. Health check interval is configurable
2. Default value maintains backward compatibility (30s)
3. Invalid values are rejected with clear error
4. Config file documents the new option
