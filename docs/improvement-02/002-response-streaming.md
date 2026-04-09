# Task 002: Response Body Streaming

**Priority**: P0 (Critical)  
**Assigned To**: Engineer B  
**Estimated Effort**: 3-4 hours  
**Dependencies**: None  

**Files to Modify**:
- `internal/usecase/proxy.go`
- `internal/usecase/proxy_test.go`

---

## Problem Statement

The current proxy reads the **entire response body into memory** before forwarding it to the client:

```go
// internal/usecase/proxy.go:212
body, err := io.ReadAll(httpResp.Body)
```

This is a critical bottleneck for 10K RPS because:

1. **Memory pressure**: Each response allocates a byte slice. At 10K RPS with average 10KB response, that's 100MB/s of allocations
2. **GC pressure**: Go's garbage collector must sweep all these allocations. GC pauses increase with heap size
3. **Time to first byte (TTFB)**: Client waits until the entire response is read before receiving any data
4. **Large response failure**: A single 100MB response will OOM the proxy

### Why This Matters for 10K RPS

At 10K RPS:
- **10KB avg response** → 100MB/s allocation rate → GC runs every ~100ms → tail latency spikes
- **Concurrent buffer memory** → With 100ms avg latency, ~1000 concurrent responses → 10GB memory for buffers
- **io.ReadAll** allocates growing buffers → each response triggers multiple allocations + copies

---

## Solution: Stream Responses Directly

Replace buffered response forwarding with `io.Copy` streaming for non-singleflight requests. For singleflight (GET dedup), keep buffering since the response must be shared.

### Step 1: Add Streaming Response Path

**File**: `internal/usecase/proxy.go`

Create a new method for streaming that bypasses the buffer:

```go
// streamResponse streams the backend response directly to the client
func (p *HTTPProxy) streamResponse(w http.ResponseWriter, httpResp *http.Response) error {
    // Copy headers
    for k, v := range httpResp.Header {
        w.Header()[k] = v
    }
    w.WriteHeader(httpResp.StatusCode)

    // Stream body directly without buffering
    if httpResp.Body != nil {
        buf := streamBufPool.Get().([]byte)
        defer streamBufPool.Put(buf)
        _, err := io.CopyBuffer(w, httpResp.Body, buf)
        if err != nil {
            return fmt.Errorf("failed to stream response body: %w", err)
        }
    }
    return nil
}
```

### Step 2: Add a Buffer Pool

**File**: `internal/usecase/proxy.go`

Add a `sync.Pool` at package level to reuse streaming buffers:

```go
import "sync"

// streamBufPool provides reusable buffers for response streaming
// Each buffer is 32KB which is optimal for io.CopyBuffer
var streamBufPool = sync.Pool{
    New: func() interface{} {
        buf := make([]byte, 32*1024) // 32KB
        return buf
    },
}
```

### Step 3: Refactor ForwardRequest for Streaming

**File**: `internal/usecase/proxy.go`

Modify `ForwardRequest` to stream for non-singleflight, and buffer only for singleflight:

**Current code** (lines 142-176):
```go
// Use singleflight only for GET requests if enabled
if p.enableSingleflight && r.Method == "GET" {
    key := backendURL + r.URL.Path
    result, err, _ := p.sf.Do(key, func() (interface{}, error) {
        return p.doRequest(r, backendURL, endpoint)
    })
    if err != nil {
        return err
    }
    proxyResp := result.(*ProxyResponse)
    for k, v := range proxyResp.Header {
        w.Header()[k] = v
    }
    w.WriteHeader(proxyResp.StatusCode)
    w.Write(proxyResp.Body)
    return nil
}

proxyResp, err := p.doRequest(r, backendURL, endpoint)
if err != nil {
    return err
}
for k, v := range proxyResp.Header {
    w.Header()[k] = v
}
w.WriteHeader(proxyResp.StatusCode)
w.Write(proxyResp.Body)
return nil
```

**Replace with**:
```go
// Use singleflight only for GET requests if enabled
// Singleflight REQUIRES buffering since response is shared across waiters
if p.enableSingleflight && r.Method == "GET" {
    key := backendURL + r.URL.Path
    result, err, _ := p.sf.Do(key, func() (interface{}, error) {
        return p.doBufferedRequest(r, backendURL, endpoint)
    })
    if err != nil {
        return err
    }
    proxyResp := result.(*ProxyResponse)
    for k, v := range proxyResp.Header {
        w.Header()[k] = v
    }
    w.WriteHeader(proxyResp.StatusCode)
    w.Write(proxyResp.Body)
    return nil
}

// For non-singleflight requests, stream directly to avoid buffering
return p.doStreamingRequest(w, r, backendURL, endpoint)
```

### Step 4: Create Streaming Request Method

**File**: `internal/usecase/proxy.go`

Add `doStreamingRequest` that performs the HTTP request and streams the response:

```go
// doStreamingRequest performs the HTTP request and streams the response directly to the client
func (p *HTTPProxy) doStreamingRequest(w http.ResponseWriter, r *http.Request, backendURL, endpoint string) (err error) {
    defer func() {
        if rec := recover(); rec != nil {
            log.Printf("Panic in doStreamingRequest: %v\nStack trace:\n%s", rec, debug.Stack())
            err = fmt.Errorf("internal panic: %v", rec)
        }
    }()

    target, err := url.Parse(backendURL)
    if err != nil {
        p.cbManager.RecordFailure(backendURL)
        http.Error(w, "Internal Server Error", http.StatusInternalServerError)
        return errors.NewInternalError("failed to parse backend URL", err)
    }

    req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String()+r.URL.Path, r.Body)
    if err != nil {
        p.cbManager.RecordFailure(backendURL)
        http.Error(w, "Internal Server Error", http.StatusInternalServerError)
        return errors.NewInternalError("failed to create request", err)
    }

    for k, v := range r.Header {
        if !isHeaderAllowed(k) {
            continue
        }
        req.Header[k] = v
    }

    httpResp, err := p.httpClient.Do(req)
    if err != nil {
        p.cbManager.RecordFailure(backendURL)
        http.Error(w, "Internal Server Error", http.StatusInternalServerError)
        return errors.NewInternalError("failed to execute request", err)
    }
    defer httpResp.Body.Close()

    // Record metrics based on status
    if httpResp.StatusCode >= 500 {
        p.cbManager.RecordFailure(backendURL)
    } else {
        p.cbManager.RecordSuccess(backendURL)
        metrics.TrafficSuccess.WithLabelValues(backendURL, endpoint).Inc()
    }

    // Stream response directly to client
    return p.streamResponse(w, httpResp)
}
```

### Step 5: Rename Existing doRequest to doBufferedRequest

**File**: `internal/usecase/proxy.go`

Rename the existing `doRequest` method to `doBufferedRequest` to clarify its purpose:

**FIND**:
```go
func (p *HTTPProxy) doRequest(r *http.Request, backendURL, endpoint string) (resp *ProxyResponse, err error) {
```

**REPLACE** with:
```go
// doBufferedRequest performs the HTTP request and buffers the entire response.
// Used only for singleflight where the response must be shared across multiple waiters.
func (p *HTTPProxy) doBufferedRequest(r *http.Request, backendURL, endpoint string) (resp *ProxyResponse, err error) {
```

### Step 6: Update Tests

**File**: `internal/usecase/proxy_test.go`

Add a test for streaming behavior:

```go
func TestProxy_StreamingResponse(t *testing.T) {
    // Create a backend that returns a large response
    largeBody := strings.Repeat("a", 1024*1024) // 1MB
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/plain")
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(largeBody))
    }))
    defer server.Close()

    // Setup proxy with singleflight DISABLED to test streaming path
    // ... (setup cbManager, rlManager, lb as in existing tests) ...

    proxy := NewHTTPProxy(cbManager, rlManager, lb, false, 30*time.Second)

    req := httptest.NewRequest("POST", server.URL+"/test", nil)
    w := httptest.NewRecorder()

    err := proxy.ForwardRequest(w, req, "/test")
    assert.NoError(t, err)
    assert.Equal(t, http.StatusOK, w.Code)
    assert.Equal(t, len(largeBody), w.Body.Len())
}
```

---

## Verification Checklist

- [ ] `streamBufPool` added with 32KB buffers
- [ ] `streamResponse` method created
- [ ] `doStreamingRequest` method created
- [ ] `doRequest` renamed to `doBufferedRequest`
- [ ] `ForwardRequest` uses streaming for non-singleflight
- [ ] Streaming test added and passing
- [ ] All existing tests pass: `go test ./...`
- [ ] No race conditions: `go test -race ./...`

---

## Performance Impact

| Metric | Before (Buffered) | After (Streaming) |
|--------|-------------------|-------------------|
| Memory per request | ~response size | ~32KB (fixed buffer) |
| TTFB | After full download | After headers parsed |
| Max response size | Limited by memory | Unlimited |
| GC pressure | High (many allocs) | Low (pooled buffers) |

---

## Important Notes

1. **Singleflight still buffers**: This is by design. Singleflight deduplicates identical GET requests, so the response must be fully read and shared. This is acceptable because singleflight reduces total requests.
2. **Error handling differs**: When streaming, we cannot send HTTP error codes after streaming has started (headers already sent). Circuit breaker recording happens before streaming begins.
3. **sync.Pool usage**: Buffers are pooled and reused, eliminating per-request allocation for the streaming path.
