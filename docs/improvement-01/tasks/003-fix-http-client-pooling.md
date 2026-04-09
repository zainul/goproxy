# Task 003: Fix HTTP Client Connection Pooling

**Priority**: P0 (Critical)  
**Estimated Complexity**: Low  
**Files to Modify**: 
- `internal/usecase/proxy.go`

---

## Problem Statement

A new `http.Client` is created for **every single request**, preventing connection reuse:

```go
// internal/usecase/proxy.go:141-145
func (pm *HTTPProxy) doRequest(req *http.Request) (*http.Response, error) {
    client := &http.Client{
        Timeout: 30 * time.Second,
    }
    return client.Do(req)
}
```

**Impact**: Each request creates a new TCP connection, causing:
- High latency (TCP handshake on every request)
- Port exhaustion under load
- Unnecessary CPU/memory overhead

---

## Solution

Create a shared `http.Client` with connection pooling that is reused across all requests.

---

## Step-by-Step Instructions

### Step 1: Create Shared HTTP Client

**File**: `internal/usecase/proxy.go`

**What to change**:

1. **Add shared client as package-level variable**:
   
   **ADD** at the top of the file (after imports):
   ```go
   var defaultHTTPClient = &http.Client{
       Timeout: 30 * time.Second,
       Transport: &http.Transport{
           MaxIdleConns:        100,
           MaxIdleConnsPerHost: 10,
           IdleConnTimeout:     90 * time.Second,
           TLSHandshakeTimeout: 10 * time.Second,
       },
   }
   ```

2. **Update doRequest to use shared client**:
   
   **FIND**:
   ```go
   func (pm *HTTPProxy) doRequest(req *http.Request) (*http.Response, error) {
       client := &http.Client{
           Timeout: 30 * time.Second,
       }
       return client.Do(req)
   }
   ```
   
   **REPLACE** with:
   ```go
   func (pm *HTTPProxy) doRequest(req *http.Request) (*http.Response, error) {
       return defaultHTTPClient.Do(req)
   }
   ```

---

### Step 2: Make Timeout Configurable (Optional but Recommended)

**File**: `internal/usecase/proxy.go`

**What to change**:

1. **Add timeout field to HTTPProxy struct**:
   
   **FIND**:
   ```go
   type HTTPProxy struct {
       cbManager        CircuitBreakerManager
       rateLimiter      RateLimiterUsecase
       loadBalancer     *entity.LoadBalancer
   }
   ```
   
   **REPLACE** with:
   ```go
   type HTTPProxy struct {
       cbManager        CircuitBreakerManager
       rateLimiter      RateLimiterUsecase
       loadBalancer     *entity.LoadBalancer
       httpClient       *http.Client
   }
   ```

2. **Update NewHTTPProxy to accept timeout**:
   
   **FIND**:
   ```go
   func NewHTTPProxy(
       cbManager CircuitBreakerManager,
       rateLimiter RateLimiterUsecase,
       lb *entity.LoadBalancer,
   ) *HTTPProxy {
       return &HTTPProxy{
           cbManager:    cbManager,
           rateLimiter:  rateLimiter,
           loadBalancer: lb,
       }
   }
   ```
   
   **REPLACE** with:
   ```go
   func NewHTTPProxy(
       cbManager CircuitBreakerManager,
       rateLimiter RateLimiterUsecase,
       lb *entity.LoadBalancer,
       timeout time.Duration,
   ) *HTTPProxy {
       return &HTTPProxy{
           cbManager:    cbManager,
           rateLimiter:  rateLimiter,
           loadBalancer: lb,
           httpClient: &http.Client{
               Timeout: timeout,
               Transport: &http.Transport{
                   MaxIdleConns:        100,
                   MaxIdleConnsPerHost: 10,
                   IdleConnTimeout:     90 * time.Second,
                   TLSHandshakeTimeout: 10 * time.Second,
               },
           },
       }
   }
   ```

3. **Update doRequest to use instance client**:
   
   **FIND**:
   ```go
   func (pm *HTTPProxy) doRequest(req *http.Request) (*http.Response, error) {
       return defaultHTTPClient.Do(req)
   }
   ```
   
   **REPLACE** with:
   ```go
   func (pm *HTTPProxy) doRequest(req *http.Request) (*http.Response, error) {
       return pm.httpClient.Do(req)
   }
   ```

---

### Step 3: Update main.go

**File**: `cmd/main.go`

**What to change**:

1. **Add timeout to proxy initialization**:
   
   **FIND**:
   ```go
   proxy := usecase.NewHTTPProxy(cbManager, rateLimiter, loadBalancer)
   ```
   
   **REPLACE** with:
   ```go
   proxy := usecase.NewHTTPProxy(cbManager, rateLimiter, loadBalancer, 30*time.Second)
   ```

---

### Step 4: Update Tests

**File**: `internal/usecase/proxy_test.go`

**What to change**:

1. **Update all proxy instantiations**:
   
   **FIND** all instances of:
   ```go
   proxy := NewHTTPProxy(cbManager, rateLimiter, lb)
   ```
   
   **REPLACE** with:
   ```go
   proxy := NewHTTPProxy(cbManager, rateLimiter, lb, 5*time.Second)
   ```

2. **Add import if missing**:
   ```go
   import "time"
   ```

---

### Step 5: Run Tests

```bash
# Run all tests
go test ./...

# Run proxy tests with verbose output
go test ./internal/usecase -run TestProxy -v

# Check for race conditions
go test -race ./...
```

**Expected result**: All tests pass, potentially faster due to connection reuse.

---

## Verification Checklist

- [ ] Shared HTTP client created with connection pooling
- [ ] `doRequest()` uses shared/instance client instead of creating new one
- [ ] Timeout is configurable via constructor parameter
- [ ] `main.go` passes timeout to proxy constructor
- [ ] All tests updated with timeout parameter
- [ ] All tests pass: `go test ./...`
- [ ] No race conditions: `go test -race ./...`

---

## Connection Pool Settings Explained

| Setting | Value | Purpose |
|---------|-------|---------|
| `MaxIdleConns` | 100 | Total idle connections across all hosts |
| `MaxIdleConnsPerHost` | 10 | Idle connections per backend host |
| `IdleConnTimeout` | 90s | How long idle connections are kept alive |
| `TLSHandshakeTimeout` | 10s | Max time for TLS handshake |

---

## Success Criteria

1. HTTP connections are reused across requests
2. No TCP connection exhaustion under load
3. Configurable timeout per proxy instance
4. All tests pass
5. Measurable latency improvement under load

---

## Performance Testing (Optional)

Run a simple benchmark to verify improvement:

```bash
go test ./internal/usecase -bench=BenchmarkForwardRequest -benchmem
```

Compare before/after metrics for:
- Requests per second
- Average latency
- Memory allocations
