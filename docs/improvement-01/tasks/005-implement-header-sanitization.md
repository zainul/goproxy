# Task 005: Implement Header Sanitization

**Priority**: P1 (High)  
**Estimated Complexity**: Low  
**Files to Modify**: 
- `internal/usecase/proxy.go`
- `pkg/constants/constants.go`

---

## Problem Statement

All request headers are forwarded to backends without filtering:

```go
// internal/usecase/proxy.go:125-131
for key, values := range req.Header {
    for _, value := range values {
        proxyReq.Header.Add(key, value)
    }
}
```

**Security risks**:
- `Authorization` headers may leak credentials to wrong backends
- `Cookie` headers can enable session hijacking
- `X-Forwarded-For` can be spoofed
- `Host` header injection attacks

---

## Solution

Implement header allowlist/blocklist to control which headers are forwarded.

---

## Step-by-Step Instructions

### Step 1: Add Header Constants

**File**: `pkg/constants/constants.go`

**ADD** these constants:

```go
// Headers to block from forwarding
var BlockedHeaders = []string{
    "Connection",
    "Keep-Alive",
    "Proxy-Authenticate",
    "Proxy-Authorization",
    "Te",
    "Trailers",
    "Transfer-Encoding",
    "Upgrade",
}

// Headers to allow forwarding (if empty, all non-blocked headers are allowed)
var AllowedHeaders = []string{
    "Accept",
    "Accept-Encoding",
    "Accept-Language",
    "Authorization",
    "Cache-Control",
    "Content-Type",
    "Date",
    "If-Match",
    "If-Modified-Since",
    "If-None-Match",
    "If-Range",
    "If-Unmodified-Since",
    "Origin",
    "Range",
    "Referer",
    "User-Agent",
}
```

---

### Step 2: Create Header Filtering Function

**File**: `internal/usecase/proxy.go`

**ADD** this helper function:

```go
import (
    "strings"
    "github.com/zainul/goproxy/pkg/constants"
)

func isHeaderAllowed(header string) bool {
    // Check if header is in blocked list
    for _, blocked := range constants.BlockedHeaders {
        if strings.EqualFold(header, blocked) {
            return false
        }
    }
    
    // If allowed list is empty, allow all non-blocked headers
    if len(constants.AllowedHeaders) == 0 {
        return true
    }
    
    // Check if header is in allowed list
    for _, allowed := range constants.AllowedHeaders {
        if strings.EqualFold(header, allowed) {
            return true
        }
    }
    
    return false
}
```

---

### Step 3: Update Header Copying Logic

**File**: `internal/usecase/proxy.go`

**FIND** this code:
```go
// Copy headers
for key, values := range req.Header {
    for _, value := range values {
        proxyReq.Header.Add(key, value)
    }
}
```

**REPLACE** with:
```go
// Copy headers with sanitization
for key, values := range req.Header {
    if !isHeaderAllowed(key) {
        continue
    }
    for _, value := range values {
        proxyReq.Header.Add(key, value)
    }
}
```

---

### Step 4: Add Tests

**File**: `internal/usecase/proxy_test.go`

**ADD** this test function:

```go
func TestHeaderSanitization(t *testing.T) {
    var receivedHeaders map[string]string
    var mu sync.Mutex

    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        mu.Lock()
        receivedHeaders = make(map[string]string)
        for key, values := range r.Header {
            if len(values) > 0 {
                receivedHeaders[key] = values[0]
            }
        }
        mu.Unlock()
        w.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    // Create request with mixed headers
    req := httptest.NewRequest(http.MethodGet, server.URL+"/test", nil)
    req.Header.Set("Authorization", "Bearer secret-token")
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("User-Agent", "TestClient/1.0")
    req.Header.Set("Connection", "keep-alive")
    req.Header.Set("X-Custom-Header", "custom-value")

    // Setup mocks
    cbManager := new(MockCircuitBreakerManager)
    cbManager.On("CanExecute", server.URL).Return(true)
    cbManager.On("RecordSuccess", server.URL)

    rateLimiter := new(MockRateLimiterUsecase)
    rateLimiter.On("Allow", server.URL).Return(true)
    rateLimiter.On("AllowEndpoint", server.URL, "/test", "GET").Return(true)

    backends := []*entity.Backend{
        {URL: server.URL, IsHealthy: true, IsReady: true, SuccessRate: 1.0},
    }
    lb := entity.NewLoadBalancer(backends)

    proxy := NewHTTPProxy(cbManager, rateLimiter, lb, 5*time.Second)
    proxy.ForwardRequest(req)

    // Assert blocked headers are not forwarded
    mu.Lock()
    defer mu.Unlock()

    assert.NotContains(t, receivedHeaders, "Connection")
    assert.Contains(t, receivedHeaders, "Authorization")
    assert.Contains(t, receivedHeaders, "Content-Type")
    assert.Contains(t, receivedHeaders, "User-Agent")
}
```

---

### Step 5: Run Tests

```bash
# Run header sanitization test
go test ./internal/usecase -run TestHeaderSanitization -v

# Run all tests
go test ./...

# Check for race conditions
go test -race ./...
```

---

## Verification Checklist

- [ ] `BlockedHeaders` constant added to `pkg/constants/constants.go`
- [ ] `AllowedHeaders` constant added to `pkg/constants/constants.go`
- [ ] `isHeaderAllowed()` function created in `internal/usecase/proxy.go`
- [ ] Header copying logic updated to use sanitization
- [ ] Test for header sanitization added
- [ ] All tests pass: `go test ./...`
- [ ] No race conditions: `go test -race ./...`

---

## Success Criteria

1. Blocked headers are never forwarded
2. Allowed headers are forwarded correctly
3. Case-insensitive header matching
4. Test verifies sanitization behavior
5. No performance regression

---

## Configuration Options (Future)

Consider making header lists configurable via config file:

```json
{
  "proxy": {
    "blocked_headers": ["Connection", "Proxy-Authorization"],
    "allowed_headers": ["Authorization", "Content-Type"]
  }
}
```
