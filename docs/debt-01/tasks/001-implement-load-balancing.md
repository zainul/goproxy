# Task 001: Implement Load Balancing

**Priority**: P0 (Critical)  
**Estimated Complexity**: Medium  
**Files to Modify**: 
- `cmd/main.go`
- `internal/usecase/proxy.go`
- `internal/entity/load_balancer.go` (new file)

---

## Problem Statement

Currently, only the first backend is used regardless of how many backends are configured:

```go
// cmd/main.go:103
backend := config.Backends[0]  // Only uses first backend
```

This means multi-backend configurations are completely non-functional.

---

## Solution: Round-Robin Load Balancer

We will implement a thread-safe round-robin load balancer that distributes requests evenly across healthy backends.

---

## Step-by-Step Instructions

### Step 1: Create Load Balancer Entity

**File**: `internal/entity/load_balancer.go` (CREATE NEW FILE)

**What to create**:

```go
package entity

import (
    "sync"
    "sync/atomic"
)

type Backend struct {
    URL         string
    IsHealthy   bool
    IsReady     bool
    SuccessRate float64
}

type LoadBalancer struct {
    backends []*Backend
    current  int64
    mu       sync.RWMutex
}

func NewLoadBalancer(backends []*Backend) *LoadBalancer {
    return &LoadBalancer{
        backends: backends,
        current:  -1,
    }
}

func (lb *LoadBalancer) Next() *Backend {
    lb.mu.RLock()
    defer lb.mu.RUnlock()

    if len(lb.backends) == 0 {
        return nil
    }

    // Get healthy backends
    healthy := lb.getHealthyBackends()
    if len(healthy) == 0 {
        // Fallback: return any backend if none are healthy
        return lb.backends[0]
    }

    // Round-robin across healthy backends
    idx := atomic.AddInt64(&lb.current, 1)
    return healthy[idx%int64(len(healthy))]
}

func (lb *LoadBalancer) getHealthyBackends() []*Backend {
    var healthy []*Backend
    for _, b := range lb.backends {
        if b.IsHealthy {
            healthy = append(healthy, b)
        }
    }
    return healthy
}

func (lb *LoadBalancer) UpdateBackends(backends []*Backend) {
    lb.mu.Lock()
    defer lb.mu.Unlock()
    lb.backends = backends
    atomic.StoreInt64(&lb.current, -1)
}
```

**Why this approach**:
- Atomic operations for thread-safe counter increment
- Read-write mutex for backend list updates
- Automatically skips unhealthy backends
- Falls back to first backend if all are unhealthy

---

### Step 2: Update Proxy to Accept Load Balancer

**File**: `internal/usecase/proxy.go`

**What to change**:

1. **Add import**:
   ```go
   import "github.com/zainul/goproxy/internal/entity"
   ```

2. **Modify HTTPProxy struct** - Add load balancer field:
   ```go
   type HTTPProxy struct {
       cbManager        CircuitBreakerManager
       rateLimiter      RateLimiterUsecase
       singleflightGroup singleflight.Group
       loadBalancer     *entity.LoadBalancer  // ADD THIS LINE
   }
   ```

3. **Modify NewHTTPProxy function signature**:
   ```go
   func NewHTTPProxy(
       cbManager CircuitBreakerManager,
       rateLimiter RateLimiterUsecase,
       lb *entity.LoadBalancer,  // ADD THIS PARAMETER
   ) *HTTPProxy {
       return &HTTPProxy{
           cbManager:    cbManager,
           rateLimiter:  rateLimiter,
           loadBalancer: lb,  // ADD THIS LINE
       }
   }
   ```

4. **Modify ForwardRequest method** - Replace backend selection:
   
   **FIND** this code:
   ```go
   func (pm *HTTPProxy) ForwardRequest(req *http.Request, backendURL string) (*http.Response, error) {
   ```
   
   **REPLACE** with:
   ```go
   func (pm *HTTPProxy) ForwardRequest(req *http.Request) (*http.Response, error) {
   ```

   **FIND** this code (inside ForwardRequest):
   ```go
   backend := &entity.Backend{
       URL: backendURL,
   }
   ```
   
   **REPLACE** with:
   ```go
   backend := pm.loadBalancer.Next()
   if backend == nil {
       return nil, errors.NewInternalError("No backends available")
   }
   ```

---

### Step 3: Update main.go to Initialize Load Balancer

**File**: `cmd/main.go`

**What to change**:

1. **Add import**:
   ```go
   import "github.com/zainul/goproxy/internal/entity"
   ```

2. **Create load balancer** - Add after backend configuration loading:
   
   **FIND** this code:
   ```go
   backend := config.Backends[0]
   ```
   
   **REPLACE** with:
   ```go
   // Create load balancer with all backends
   var backends []*entity.Backend
   for _, b := range config.Backends {
       backends = append(backends, &entity.Backend{
           URL:         b.URL,
           IsHealthy:   b.IsHealthy,
           IsReady:     b.IsReady,
           SuccessRate: b.SuccessRate,
       })
   }
   loadBalancer := entity.NewLoadBalancer(backends)
   ```

3. **Update proxy initialization**:
   
   **FIND** this code:
   ```go
   proxy := usecase.NewHTTPProxy(cbManager, rateLimiter)
   ```
   
   **REPLACE** with:
   ```go
   proxy := usecase.NewHTTPProxy(cbManager, rateLimiter, loadBalancer)
   ```

4. **Update HTTP handler** - Remove backend parameter:
   
   **FIND** this code:
   ```go
   response, err := proxy.ForwardRequest(r, backend.URL)
   ```
   
   **REPLACE** with:
   ```go
   response, err := proxy.ForwardRequest(r)
   ```

---

### Step 4: Update Tests

**File**: `internal/usecase/proxy_test.go`

**What to change**:

1. **Add import**:
   ```go
   import "github.com/zainul/goproxy/internal/entity"
   ```

2. **Update test setup** - Create load balancer in tests:
   
   **FIND** all instances of:
   ```go
   proxy := NewHTTPProxy(cbManager, rateLimiter)
   ```
   
   **REPLACE** with:
   ```go
   backends := []*entity.Backend{
       {URL: server.URL, IsHealthy: true, IsReady: true, SuccessRate: 1.0},
   }
   lb := entity.NewLoadBalancer(backends)
   proxy := NewHTTPProxy(cbManager, rateLimiter, lb)
   ```

3. **Update function calls** - Remove backend URL parameter:
   
   **FIND** all instances of:
   ```go
   proxy.ForwardRequest(req, server.URL)
   ```
   
   **REPLACE** with:
   ```go
   proxy.ForwardRequest(req)
   ```

---

### Step 5: Run Tests

Execute these commands in order:

```bash
# Run all tests
go test ./...

# Run specific proxy tests
go test ./internal/usecase -run TestProxy -v

# Check for race conditions
go test -race ./...
```

**Expected result**: All tests pass with no race conditions detected.

---

## Verification Checklist

- [ ] `internal/entity/load_balancer.go` created with LoadBalancer struct
- [ ] `internal/usecase/proxy.go` updated to use load balancer
- [ ] `cmd/main.go` initializes load balancer with all backends
- [ ] `internal/usecase/proxy_test.go` updated with load balancer
- [ ] All tests pass: `go test ./...`
- [ ] No race conditions: `go test -race ./...`
- [ ] Remove TODO comment in `cmd/main.go` about load balancing

---

## Success Criteria

1. Requests are distributed across multiple backends
2. Unhealthy backends are automatically skipped
3. Thread-safe under concurrent load
4. All existing tests pass
5. No performance regression

---

## Future Enhancements (Out of Scope for This Task)

- Least-connections algorithm
- Weighted round-robin
- Health-check-based backend removal
- Sticky sessions
