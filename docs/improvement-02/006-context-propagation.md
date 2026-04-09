# Task 006: Request Context Propagation & Per-Backend Timeouts

**Priority**: P1 (High)  
**Assigned To**: Engineer B  
**Estimated Effort**: 3-4 hours  
**Dependencies**: Task 002 (Response Streaming) should be completed first, since both modify `proxy.go`  

**Files to Modify**:
- `internal/usecase/proxy.go`
- `pkg/utils/config.go`
- `config.json`
- `internal/usecase/proxy_test.go`

---

## Problem Statement

### Issue 1: No client disconnection detection

When a client disconnects, the proxy still waits for the backend response and processes it fully:

```go
// internal/usecase/proxy.go:192
req, err := http.NewRequest(r.Method, target.String()+r.URL.Path, r.Body)
```

`http.NewRequest` does NOT propagate the original request's context. When the client disconnects, the proxy has no way to know — it continues consuming backend resources.

### Issue 2: Global timeout, not per-backend

```go
// internal/usecase/proxy.go:70-78
httpClient: &http.Client{
    Timeout: timeout,   // Same timeout for ALL backends
}
```

Different backends may have different latency profiles:
- Internal service: 100ms timeout
- External API: 30s timeout
- File upload proxy: 5 minute timeout

A single global timeout is either too aggressive (dropping valid slow requests) or too lenient (not protecting against slow backends).

### Why This Matters for 10K RPS

- **Wasted goroutines**: Without context cancellation, disconnected client requests waste backend capacity
- **Backend overload**: If a backend is slow, tight per-backend timeouts let the proxy fail fast and try another backend
- **Resource leak**: At 10K RPS with even 1% zombie requests = 100 goroutines/sec leaked

---

## Solution

### Step 1: Add Per-Backend Timeout Config

**File**: `pkg/utils/config.go`

Add `Timeout` field to `BackendConfig`:

```go
type BackendConfig struct {
    // ... existing fields ...
    Timeout string `json:"timeout" yaml:"timeout"`  // Per-backend request timeout (e.g., "10s", "30s")
}
```

### Step 2: Propagate Client Context

**File**: `internal/usecase/proxy.go`

In both `doBufferedRequest` (renamed from `doRequest` in Task 002) and `doStreamingRequest`:

**FIND** (in doBufferedRequest, ~line 192):
```go
req, err := http.NewRequest(r.Method, target.String()+r.URL.Path, r.Body)
```

**REPLACE** with:
```go
req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String()+r.URL.Path, r.Body)
```

This ensures that when the client disconnects (`r.Context()` is cancelled), the backend request is also cancelled.

### Step 3: Implement Per-Backend Timeout via Context

**File**: `internal/usecase/proxy.go`

Add a per-backend timeout map to `HTTPProxy`:

```go
type HTTPProxy struct {
    cbManager          CircuitBreakerUsecase
    rlManager          RateLimiterUsecase
    lb                 *entity.LoadBalancer
    sf                 singleflight.Group
    enableSingleflight bool
    httpClient         *http.Client
    backendTimeouts    map[string]time.Duration  // per-backend timeouts
}
```

Add a method to set per-backend timeouts:

```go
// SetBackendTimeout configures a per-backend request timeout
func (p *HTTPProxy) SetBackendTimeout(backendURL string, timeout time.Duration) {
    if p.backendTimeouts == nil {
        p.backendTimeouts = make(map[string]time.Duration)
    }
    p.backendTimeouts[backendURL] = timeout
}

// getBackendTimeout returns the timeout for a specific backend
func (p *HTTPProxy) getBackendTimeout(backendURL string) time.Duration {
    if timeout, ok := p.backendTimeouts[backendURL]; ok {
        return timeout
    }
    return p.httpClient.Timeout // fallback to global
}
```

### Step 4: Apply Per-Backend Timeout in Request Path

**File**: `internal/usecase/proxy.go`

In `ForwardRequest`, after selecting the backend and before making the request, wrap the context:

```go
// Apply per-backend timeout
backendTimeout := p.getBackendTimeout(backendURL)
if backendTimeout > 0 {
    ctx, cancel := context.WithTimeout(r.Context(), backendTimeout)
    defer cancel()
    r = r.WithContext(ctx)
}
```

Place this after the circuit breaker check (line ~140) and before the singleflight/streaming section.

### Step 5: Set Timeouts from Config in main.go

**File**: `cmd/main.go`

After proxy initialization, set per-backend timeouts:

```go
// Set per-backend timeouts
for _, backend := range config.Backends {
    if backend.Timeout != "" {
        timeout, err := time.ParseDuration(backend.Timeout)
        if err != nil {
            log.Printf("Warning: invalid timeout for backend %s: %v, using default", backend.URL, err)
            continue
        }
        proxy.SetBackendTimeout(backend.URL, timeout)
        log.Printf("Backend %s timeout: %s", backend.URL, timeout)
    }
}
```

### Step 6: Update config.json

**File**: `config.json`

Add timeout to backend config:

```json
{
  "backends": [
    {
      "url": "http://httpbin.org",
      "timeout": "15s",
      ...
    }
  ]
}
```

### Step 7: Add Validation

**File**: `pkg/utils/config.go`

In `ValidateConfig`:

```go
// Validate backend timeout
if backend.Timeout != "" {
    _, err := time.ParseDuration(backend.Timeout)
    if err != nil {
        errors = append(errors, fmt.Sprintf("backends[%d].timeout is invalid: %v", i, err))
    }
}
```

### Step 8: Update Tests

**File**: `internal/usecase/proxy_test.go`

Add tests for context cancellation and per-backend timeouts:

```go
func TestProxy_ClientDisconnect_CancelsBackendRequest(t *testing.T) {
    // Backend that takes 5 seconds to respond
    backendCalled := make(chan struct{})
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        close(backendCalled)
        <-r.Context().Done()  // Wait for cancellation
    }))
    defer server.Close()

    // ... setup proxy ...

    // Create a cancellable context
    ctx, cancel := context.WithCancel(context.Background())
    req := httptest.NewRequest("GET", server.URL+"/slow", nil).WithContext(ctx)
    w := httptest.NewRecorder()

    // Start proxy in goroutine
    done := make(chan error)
    go func() {
        done <- proxy.ForwardRequest(w, req, "/slow")
    }()

    // Wait for backend to be called, then cancel
    <-backendCalled
    cancel()

    // Proxy should return quickly after cancellation
    select {
    case err := <-done:
        assert.Error(t, err) // Should have context cancelled error
    case <-time.After(2 * time.Second):
        t.Fatal("Proxy did not return after client disconnect")
    }
}

func TestProxy_PerBackendTimeout(t *testing.T) {
    // Backend that takes 2 seconds to respond
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        time.Sleep(2 * time.Second)
        w.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    // ... setup proxy with 500ms per-backend timeout ...
    proxy.SetBackendTimeout(server.URL, 500*time.Millisecond)

    req := httptest.NewRequest("GET", server.URL+"/slow", nil)
    w := httptest.NewRecorder()

    start := time.Now()
    err := proxy.ForwardRequest(w, req, "/slow")
    elapsed := time.Since(start)

    assert.Error(t, err)
    assert.Less(t, elapsed, 1*time.Second, "Should timeout in ~500ms, not wait full 2s")
}
```

---

## Verification Checklist

- [ ] `http.NewRequest` replaced with `http.NewRequestWithContext` in all request paths
- [ ] Per-backend timeout map added to `HTTPProxy`
- [ ] `SetBackendTimeout` and `getBackendTimeout` methods added
- [ ] Context wrapping with per-backend timeout in `ForwardRequest`
- [ ] `BackendConfig.Timeout` field added to config
- [ ] Validation added for timeout duration
- [ ] `cmd/main.go` sets per-backend timeouts from config
- [ ] Context cancellation test passes
- [ ] Per-backend timeout test passes
- [ ] All existing tests pass: `go test ./...`
- [ ] No race conditions: `go test -race ./...`

---

## Success Criteria

1. Client disconnection cancels in-flight backend request within 100ms
2. Per-backend timeouts correctly override global timeout
3. Timeout errors are properly recorded in circuit breaker (failure)
4. No goroutine leaks from cancelled requests
