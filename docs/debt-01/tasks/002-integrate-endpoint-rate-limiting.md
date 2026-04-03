# Task 002: Integrate Endpoint Rate Limiting

**Priority**: P0 (Critical)  
**Estimated Complexity**: Medium  
**Files to Modify**: 
- `internal/usecase/proxy.go`
- `internal/usecase/proxy_test.go`

---

## Problem Statement

The `AllowEndpoint()` method exists in `RateLimiterManager` but is **never called** in the proxy flow. This means endpoint-specific rate limits configured in `config.json` are completely ignored.

**Current code** (`internal/usecase/proxy.go:68-74`):
```go
// Check rate limit (backend-wide)
if !pm.rateLimiter.Allow(backend.URL) {
    metrics.RecordTrafficBlocked("rate_limit")
    http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
    return
}

// NOTE: Endpoint-specific rate limiting is not implemented here
// because we don't have access to the endpoint path at this stage
```

---

## Solution

Pass the request path to the rate limiting logic and call `AllowEndpoint()` for configured endpoints.

---

## Step-by-Step Instructions

### Step 1: Understand Current Rate Limiter Interface

**File**: `internal/usecase/rate_limiter.go`

The interface already has the method we need:

```go
type RateLimiterUsecase interface {
    Allow(key string) bool
    AllowEndpoint(key, endpoint, method string) bool  // THIS EXISTS
    GetDynamicThreshold() DynamicThreshold
}
```

**What it does**:
- `key`: Usually the backend URL or client identifier
- `endpoint`: The request path (e.g., "/api/users")
- `method`: HTTP method (e.g., "GET", "POST")
- Returns `true` if request is allowed, `false` if rate limited

---

### Step 2: Update ForwardRequest Method

**File**: `internal/usecase/proxy.go`

**What to change**:

1. **Modify ForwardRequest signature** to accept the request path and method:
   
   **FIND**:
   ```go
   func (pm *HTTPProxy) ForwardRequest(req *http.Request) (*http.Response, error) {
   ```
   
   **REPLACE** with:
   ```go
   func (pm *HTTPProxy) ForwardRequest(req *http.Request) (*http.Response, error) {
       endpoint := req.URL.Path
       method := req.Method
   ```

2. **Add endpoint rate limiting check** after backend-wide rate limit:
   
   **FIND** this code block:
   ```go
   // Check rate limit (backend-wide)
   if !pm.rateLimiter.Allow(backend.URL) {
       metrics.RecordTrafficBlocked("rate_limit")
       return nil, errors.NewRateLimitExceededError("Rate limit exceeded")
   }
   ```
   
   **ADD AFTER** it:
   ```go
   // Check endpoint-specific rate limit
   if !pm.rateLimiter.AllowEndpoint(backend.URL, endpoint, method) {
       metrics.RecordTrafficBlocked("endpoint_rate_limit")
       return nil, errors.NewRateLimitExceededError("Endpoint rate limit exceeded")
   }
   ```

---

### Step 3: Update Metrics Recording

**File**: `internal/usecase/proxy.go`

**What to change**:

1. **Update success metrics** to include endpoint:
   
   **FIND**:
   ```go
   // TODO: We don't have endpoint info here, using empty string
   metrics.RecordTrafficSuccess("", 1)
   ```
   
   **REPLACE** with:
   ```go
   metrics.RecordTrafficSuccess(endpoint, 1)
   ```

2. **Update failure metrics** to include endpoint:
   
   **FIND**:
   ```go
   metrics.RecordTrafficBlocked("circuit_breaker")
   ```
   
   **REPLACE** with:
   ```go
   metrics.RecordTrafficBlocked("circuit_breaker")
   // Optionally add endpoint-specific failure metric
   metrics.RecordTrafficFailure(endpoint, "circuit_breaker")
   ```

---

### Step 4: Update Tests

**File**: `internal/usecase/proxy_test.go`

**What to change**:

1. **Add endpoint rate limiting test**:
   
   **ADD** this new test function:
   ```go
   func TestForwardRequest_EndpointRateLimiting(t *testing.T) {
       // Setup mock server
       server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
           w.WriteHeader(http.StatusOK)
           w.Write([]byte("OK"))
       }))
       defer server.Close()

       // Setup mocks
       cbManager := new(MockCircuitBreakerManager)
       cbManager.On("CanExecute", server.URL).Return(true)
       cbManager.On("RecordSuccess", server.URL)

       rateLimiter := new(MockRateLimiterUsecase)
       rateLimiter.On("Allow", server.URL).Return(true)
       rateLimiter.On("AllowEndpoint", server.URL, "/api/test", "GET").Return(false)

       // Create load balancer
       backends := []*entity.Backend{
           {URL: server.URL, IsHealthy: true, IsReady: true, SuccessRate: 1.0},
       }
       lb := entity.NewLoadBalancer(backends)

       proxy := NewHTTPProxy(cbManager, rateLimiter, lb)

       // Create request
       req := httptest.NewRequest(http.MethodGet, "/api/test", nil)

       // Execute
       resp, err := proxy.ForwardRequest(req)

       // Assert - should be rate limited
       assert.Nil(t, resp)
       assert.Error(t, err)
       assert.Contains(t, err.Error(), "Endpoint rate limit exceeded")
       
       rateLimiter.AssertExpectations(t)
   }
   ```

2. **Update existing tests** - Add endpoint rate limiting mock:
   
   **FIND** all test functions that mock rate limiter:
   ```go
   rateLimiter.On("Allow", server.URL).Return(true)
   ```
   
   **ADD AFTER** each one:
   ```go
   rateLimiter.On("AllowEndpoint", server.URL, mock.Anything, mock.Anything).Return(true)
   ```

---

### Step 5: Run Tests

Execute these commands:

```bash
# Run all tests
go test ./...

# Run specific rate limiter tests
go test ./internal/usecase -run TestRateLimiter -v

# Run endpoint rate limiting test
go test ./internal/usecase -run TestForwardRequest_EndpointRateLimiting -v

# Check for race conditions
go test -race ./...
```

**Expected result**: All tests pass, including the new endpoint rate limiting test.

---

## Verification Checklist

- [ ] `ForwardRequest()` extracts endpoint and method from request
- [ ] Endpoint rate limit check added after backend-wide check
- [ ] Metrics recording includes endpoint path
- [ ] New test `TestForwardRequest_EndpointRateLimiting` added
- [ ] All existing tests updated with endpoint rate limit mocks
- [ ] All tests pass: `go test ./...`
- [ ] No race conditions: `go test -race ./...`
- [ ] Remove NOTE comment about endpoint rate limiting not implemented

---

## Success Criteria

1. Endpoint-specific rate limits from config are enforced
2. Backend-wide rate limiting still works
3. Metrics include endpoint information
4. All tests pass
5. No performance regression

---

## Testing the Feature

After implementation, you can test with this config:

```json
{
  "rate_limiter": {
    "type": "sliding_window",
    "requests": 100,
    "window": "1m",
    "endpoints": [
      {
        "path": "/api/users",
        "method": "GET",
        "requests": 10,
        "window": "1m"
      }
    ]
  }
}
```

**Expected behavior**:
- General requests: 100 per minute
- GET /api/users: 10 per minute (more restrictive)
