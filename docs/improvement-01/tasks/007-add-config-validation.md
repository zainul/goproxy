# Task 007: Add Comprehensive Configuration Validation

**Priority**: P1 (High)  
**Estimated Complexity**: Low  
**Files to Modify**: 
- `pkg/utils/config.go`
- `pkg/utils/config_test.go`

---

## Problem Statement

Configuration validation is incomplete. Only `counter_type` and `rate_limiter.type` are validated. Missing validations:

| Field | Missing Validation |
|-------|-------------------|
| `backends[].url` | URL format, empty check |
| `backends[].url` | Duplicate backend detection |
| `rate_limiter.requests` | Must be positive integer |
| `rate_limiter.window` | Must be valid duration |
| `circuit_breaker.failure_threshold` | Must be positive |
| `circuit_breaker.success_threshold` | Must be positive |
| `circuit_breaker.timeout` | Must be valid duration |
| `redis.addr` | Must not be empty |

---

## Solution

Add comprehensive validation with clear error messages.

---

## Step-by-Step Instructions

### Step 1: Add Validation Function

**File**: `pkg/utils/config.go`

**ADD** this import:
```go
import (
    "fmt"
    "net/url"
    "strings"
    "time"
)
```

**ADD** this validation function after `LoadConfig()`:

```go
func ValidateConfig(config *Config) error {
    var errors []string

    // Validate rate limiter
    if config.RateLimiter.Requests <= 0 {
        errors = append(errors, "rate_limiter.requests must be positive")
    }

    _, err := time.ParseDuration(config.RateLimiter.Window)
    if err != nil {
        errors = append(errors, fmt.Sprintf("rate_limiter.window is invalid: %v", err))
    }

    // Validate Redis config
    if config.Redis.Addr == "" {
        errors = append(errors, "redis.addr must not be empty")
    }

    // Validate circuit breaker
    if config.CircuitBreaker.FailureThreshold <= 0 {
        errors = append(errors, "circuit_breaker.failure_threshold must be positive")
    }

    if config.CircuitBreaker.SuccessThreshold <= 0 {
        errors = append(errors, "circuit_breaker.success_threshold must be positive")
    }

    _, err = time.ParseDuration(config.CircuitBreaker.Timeout)
    if err != nil {
        errors = append(errors, fmt.Sprintf("circuit_breaker.timeout is invalid: %v", err))
    }

    // Validate backends
    if len(config.Backends) == 0 {
        errors = append(errors, "at least one backend must be configured")
    }

    seenURLs := make(map[string]bool)
    for i, backend := range config.Backends {
        if backend.URL == "" {
            errors = append(errors, fmt.Sprintf("backends[%d].url must not be empty", i))
            continue
        }

        _, err := url.ParseRequestURI(backend.URL)
        if err != nil {
            errors = append(errors, fmt.Sprintf("backends[%d].url is invalid: %v", i, err))
        }

        if seenURLs[backend.URL] {
            errors = append(errors, fmt.Sprintf("backends[%d].url is duplicate: %s", i, backend.URL))
        }
        seenURLs[backend.URL] = true
    }

    // Validate endpoint rate limits
    for i, endpoint := range config.RateLimiter.Endpoints {
        if endpoint.Path == "" {
            errors = append(errors, fmt.Sprintf("rate_limiter.endpoints[%d].path must not be empty", i))
        }

        if !strings.HasPrefix(endpoint.Path, "/") {
            errors = append(errors, fmt.Sprintf("rate_limiter.endpoints[%d].path must start with /", i))
        }

        if endpoint.Method == "" {
            errors = append(errors, fmt.Sprintf("rate_limiter.endpoints[%d].method must not be empty", i))
        }

        if endpoint.Requests <= 0 {
            errors = append(errors, fmt.Sprintf("rate_limiter.endpoints[%d].requests must be positive", i))
        }

        _, err := time.ParseDuration(endpoint.Window)
        if err != nil {
            errors = append(errors, fmt.Sprintf("rate_limiter.endpoints[%d].window is invalid: %v", i, err))
        }
    }

    if len(errors) > 0 {
        return fmt.Errorf("configuration validation failed:\n  - %s", strings.Join(errors, "\n  - "))
    }

    return nil
}
```

---

### Step 2: Update LoadConfig to Call Validation

**File**: `pkg/utils/config.go`

**FIND** the end of `LoadConfig()` function:

```go
return config, nil
```

**REPLACE** with:
```go
// Validate configuration
if err := ValidateConfig(config); err != nil {
    return nil, fmt.Errorf("invalid configuration: %w", err)
}

return config, nil
```

---

### Step 3: Add Validation Tests

**File**: `pkg/utils/config_test.go`

**ADD** these test functions:

```go
func TestValidateConfig_ValidConfig(t *testing.T) {
    config := &Config{
        RateLimiter: RateLimiterConfig{
            Type:     "sliding_window",
            Requests: 100,
            Window:   "1m",
        },
        CircuitBreaker: CircuitBreakerConfig{
            FailureThreshold: 5,
            SuccessThreshold: 3,
            Timeout:          "30s",
            CounterType:      "ring_buffer",
        },
        Backends: []BackendConfig{
            {
                URL:       "http://localhost:8080",
                IsHealthy: true,
                IsReady:   true,
            },
        },
        Redis: RedisConfig{
            Addr: "localhost:6379",
        },
    }

    err := ValidateConfig(config)
    assert.NoError(t, err)
}

func TestValidateConfig_InvalidRateLimiterRequests(t *testing.T) {
    config := &Config{
        RateLimiter: RateLimiterConfig{
            Type:     "sliding_window",
            Requests: 0,
            Window:   "1m",
        },
        CircuitBreaker: CircuitBreakerConfig{
            FailureThreshold: 5,
            SuccessThreshold: 3,
            Timeout:          "30s",
            CounterType:      "ring_buffer",
        },
        Backends: []BackendConfig{
            {URL: "http://localhost:8080"},
        },
        Redis: RedisConfig{Addr: "localhost:6379"},
    }

    err := ValidateConfig(config)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "rate_limiter.requests must be positive")
}

func TestValidateConfig_InvalidBackendURL(t *testing.T) {
    config := &Config{
        RateLimiter: RateLimiterConfig{
            Type:     "sliding_window",
            Requests: 100,
            Window:   "1m",
        },
        CircuitBreaker: CircuitBreakerConfig{
            FailureThreshold: 5,
            SuccessThreshold: 3,
            Timeout:          "30s",
            CounterType:      "ring_buffer",
        },
        Backends: []BackendConfig{
            {URL: "not-a-valid-url"},
        },
        Redis: RedisConfig{Addr: "localhost:6379"},
    }

    err := ValidateConfig(config)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "backends[0].url is invalid")
}

func TestValidateConfig_DuplicateBackends(t *testing.T) {
    config := &Config{
        RateLimiter: RateLimiterConfig{
            Type:     "sliding_window",
            Requests: 100,
            Window:   "1m",
        },
        CircuitBreaker: CircuitBreakerConfig{
            FailureThreshold: 5,
            SuccessThreshold: 3,
            Timeout:          "30s",
            CounterType:      "ring_buffer",
        },
        Backends: []BackendConfig{
            {URL: "http://localhost:8080"},
            {URL: "http://localhost:8080"},
        },
        Redis: RedisConfig{Addr: "localhost:6379"},
    }

    err := ValidateConfig(config)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "backends[1].url is duplicate")
}

func TestValidateConfig_EmptyBackends(t *testing.T) {
    config := &Config{
        RateLimiter: RateLimiterConfig{
            Type:     "sliding_window",
            Requests: 100,
            Window:   "1m",
        },
        CircuitBreaker: CircuitBreakerConfig{
            FailureThreshold: 5,
            SuccessThreshold: 3,
            Timeout:          "30s",
            CounterType:      "ring_buffer",
        },
        Backends: []BackendConfig{},
        Redis:    RedisConfig{Addr: "localhost:6379"},
    }

    err := ValidateConfig(config)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "at least one backend must be configured")
}

func TestValidateConfig_InvalidEndpointPath(t *testing.T) {
    config := &Config{
        RateLimiter: RateLimiterConfig{
            Type:     "sliding_window",
            Requests: 100,
            Window:   "1m",
            Endpoints: []EndpointConfig{
                {
                    Path:     "no-slash",
                    Method:   "GET",
                    Requests: 10,
                    Window:   "1m",
                },
            },
        },
        CircuitBreaker: CircuitBreakerConfig{
            FailureThreshold: 5,
            SuccessThreshold: 3,
            Timeout:          "30s",
            CounterType:      "ring_buffer",
        },
        Backends: []BackendConfig{
            {URL: "http://localhost:8080"},
        },
        Redis: RedisConfig{Addr: "localhost:6379"},
    }

    err := ValidateConfig(config)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "rate_limiter.endpoints[0].path must start with /")
}
```

---

### Step 4: Run Tests

```bash
# Run validation tests
go test ./pkg/utils -run TestValidateConfig -v

# Run all config tests
go test ./pkg/utils -v

# Run all tests
go test ./...
```

**Expected result**: All validation tests pass.

---

## Verification Checklist

- [ ] `ValidateConfig()` function created in `pkg/utils/config.go`
- [ ] Rate limiter validation (requests, window)
- [ ] Circuit breaker validation (thresholds, timeout)
- [ ] Backend validation (URL format, duplicates, empty)
- [ ] Endpoint rate limit validation (path, method, requests, window)
- [ ] Redis validation (addr not empty)
- [ ] `LoadConfig()` calls `ValidateConfig()`
- [ ] All validation tests added
- [ ] All tests pass: `go test ./...`

---

## Success Criteria

1. Invalid configurations are rejected with clear error messages
2. All required fields are validated
3. Duplicate backends are detected
4. URL format is validated
5. Duration strings are parsed and validated
6. Tests cover all validation scenarios

---

## Error Message Format

All validation errors follow this format:
```
configuration validation failed:
  - rate_limiter.requests must be positive
  - backends[0].url is invalid: parse "not-a-url": invalid URI for request
  - redis.addr must not be empty
```
