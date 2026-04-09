# Task 004: Add Health Checker Tests

**Priority**: P1 (High)  
**Estimated Complexity**: Medium  
**Files to Create**: 
- `internal/usecase/health_checker_test.go`

---

## Problem Statement

The health checker is fully implemented in `internal/usecase/health_checker.go` but has **zero test coverage**. This is a critical gap because:
- Health checker runs as a background worker
- Failures are hard to detect without tests
- State changes affect rate limiting and circuit breaker behavior

---

## Solution

Create comprehensive unit tests covering all health checker functionality.

---

## Step-by-Step Instructions

### Step 1: Create Test File

**File**: `internal/usecase/health_checker_test.go` (CREATE NEW FILE)

**Add imports**:
```go
package usecase

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "sync"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
    "github.com/zainul/goproxy/internal/entity"
)
```

---

### Step 2: Write Test for Health Check

**ADD** this test function:

```go
func TestHealthChecker_CheckHealth_Success(t *testing.T) {
    // Create mock health endpoint
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        assert.Equal(t, "/health", r.URL.Path)
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status": "ok"}`))
    }))
    defer server.Close()

    // Create backend
    backend := &entity.Backend{
        URL:           server.URL,
        HealthEndpoint: "/health",
        IsHealthy:     false,
    }

    // Create health checker
    hc := NewHealthChecker([]*entity.Backend{backend})

    // Run health check
    hc.CheckHealth(backend)

    // Assert backend is now healthy
    assert.True(t, backend.IsHealthy)
}

func TestHealthChecker_CheckHealth_Failure(t *testing.T) {
    // Create mock server that returns 500
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusInternalServerError)
    }))
    defer server.Close()

    backend := &entity.Backend{
        URL:           server.URL,
        HealthEndpoint: "/health",
        IsHealthy:     true,
    }

    hc := NewHealthChecker([]*entity.Backend{backend})
    hc.CheckHealth(backend)

    assert.False(t, backend.IsHealthy)
}

func TestHealthChecker_CheckHealth_ConnectionRefused(t *testing.T) {
    // Use a port that's not listening
    backend := &entity.Backend{
        URL:           "http://localhost:99999",
        HealthEndpoint: "/health",
        IsHealthy:     true,
    }

    hc := NewHealthChecker([]*entity.Backend{backend})
    hc.CheckHealth(backend)

    assert.False(t, backend.IsHealthy)
}
```

---

### Step 3: Write Test for Readiness Check

**ADD** this test function:

```go
func TestHealthChecker_CheckReadiness_Success(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        assert.Equal(t, "/ready", r.URL.Path)
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"ready": true}`))
    }))
    defer server.Close()

    backend := &entity.Backend{
        URL:              server.URL,
        ReadinessEndpoint: "/ready",
        IsReady:          false,
    }

    hc := NewHealthChecker([]*entity.Backend{backend})
    hc.CheckReadiness(backend)

    assert.True(t, backend.IsReady)
}

func TestHealthChecker_CheckReadiness_Failure(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusServiceUnavailable)
    }))
    defer server.Close()

    backend := &entity.Backend{
        URL:              server.URL,
        ReadinessEndpoint: "/ready",
        IsReady:          true,
    }

    hc := NewHealthChecker([]*entity.Backend{backend})
    hc.CheckReadiness(backend)

    assert.False(t, backend.IsReady)
}
```

---

### Step 4: Write Test for Statistics Check

**ADD** this test function:

```go
func TestHealthChecker_CheckStats_Success(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        assert.Equal(t, "/stats", r.URL.Path)
        
        response := map[string]interface{}{
            "success_rate": 0.95,
            "total_requests": 1000,
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(response)
    }))
    defer server.Close()

    backend := &entity.Backend{
        URL:           server.URL,
        StatsEndpoint: "/stats",
        SuccessRate:   0.0,
    }

    hc := NewHealthChecker([]*entity.Backend{backend})
    hc.CheckStats(backend)

    assert.Equal(t, 0.95, backend.SuccessRate)
}

func TestHealthChecker_CheckStats_InvalidResponse(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`invalid json`))
    }))
    defer server.Close()

    backend := &entity.Backend{
        URL:           server.URL,
        StatsEndpoint: "/stats",
        SuccessRate:   0.8,
    }

    hc := NewHealthChecker([]*entity.Backend{backend})
    hc.CheckStats(backend)

    // Success rate should remain unchanged on parse error
    assert.Equal(t, 0.8, backend.SuccessRate)
}
```

---

### Step 5: Write Test for Start/Stop

**ADD** this test function:

```go
func TestHealthChecker_StartStop(t *testing.T) {
    callCount := 0
    var mu sync.Mutex

    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        mu.Lock()
        callCount++
        mu.Unlock()
        w.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    backend := &entity.Backend{
        URL:           server.URL,
        HealthEndpoint: "/health",
        IsHealthy:     false,
    }

    hc := NewHealthChecker([]*entity.Backend{backend})
    
    // Start with very short interval for testing
    hc.Start(100 * time.Millisecond)
    
    // Wait for a few health checks
    time.Sleep(350 * time.Millisecond)
    
    // Stop the health checker
    hc.Stop()
    
    // Verify health checks were called
    mu.Lock()
    count := callCount
    mu.Unlock()
    
    assert.GreaterOrEqual(t, count, 2)
}

func TestHealthChecker_MultipleBackends(t *testing.T) {
    server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))
    defer server1.Close()

    server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusInternalServerError)
    }))
    defer server2.Close()

    backend1 := &entity.Backend{
        URL:           server1.URL,
        HealthEndpoint: "/health",
        IsHealthy:     false,
    }

    backend2 := &entity.Backend{
        URL:           server2.URL,
        HealthEndpoint: "/health",
        IsHealthy:     true,
    }

    hc := NewHealthChecker([]*entity.Backend{backend1, backend2})
    hc.CheckAll()

    assert.True(t, backend1.IsHealthy)
    assert.False(t, backend2.IsHealthy)
}
```

---

### Step 6: Run Tests

```bash
# Run health checker tests
go test ./internal/usecase -run TestHealthChecker -v

# Run all tests
go test ./...

# Check for race conditions
go test -race ./internal/usecase
```

**Expected result**: All health checker tests pass.

---

## Verification Checklist

- [ ] `internal/usecase/health_checker_test.go` created
- [ ] Test for successful health check
- [ ] Test for failed health check
- [ ] Test for connection refused scenario
- [ ] Test for successful readiness check
- [ ] Test for failed readiness check
- [ ] Test for statistics check with valid response
- [ ] Test for statistics check with invalid response
- [ ] Test for Start/Stop lifecycle
- [ ] Test for multiple backends
- [ ] All tests pass: `go test ./...`
- [ ] No race conditions: `go test -race ./...`

---

## Success Criteria

1. All health checker methods have test coverage
2. Edge cases are tested (connection refused, invalid JSON)
3. Start/Stop lifecycle is tested
4. Multiple backend scenario is tested
5. No race conditions in concurrent health checks

---

## Notes

- Use short intervals (100ms) in tests to keep test execution fast
- Always use `httptest.NewServer` to avoid network dependencies
- Use `sync.Mutex` when counting calls from multiple goroutines
- Clean up servers with `defer server.Close()`
