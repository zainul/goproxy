# Task 001: HTTP Transport & Connection Pool Tuning

**Priority**: P0 (Critical)  
**Assigned To**: Engineer A  
**Estimated Effort**: 2-3 hours  
**Dependencies**: None  

**Files to Modify**:
- `internal/usecase/proxy.go`
- `pkg/utils/config.go`
- `config.json`

---

## Problem Statement

The current HTTP transport is configured with conservative defaults that will bottleneck at high RPS:

```go
// internal/usecase/proxy.go:72-76
Transport: &http.Transport{
    MaxIdleConns:        100,
    MaxIdleConnsPerHost: 10,    // Only 10 idle connections per backend
    IdleConnTimeout:     90 * time.Second,
},
```

At 10,000 RPS with even 10ms average response time, we need ~100 concurrent connections per backend. With `MaxIdleConnsPerHost: 10`, most connections are created and destroyed on every request, wasting TCP handshake time and exhausting ephemeral ports.

### Why This Matters for 10K RPS

- **TCP handshake overhead**: Creating a new connection costs ~1-3ms (local) or ~50-100ms (cross-DC)
- **Ephemeral port exhaustion**: Destroyed connections enter TIME_WAIT (2 minutes on Linux), at 10K RPS you exhaust ports in seconds
- **File descriptor limits**: Each connection uses an FD, new connections spike FD usage
- **TLS overhead**: If backends use HTTPS, each new connection costs an additional TLS handshake (~5-10ms)

---

## Solution

### Step 1: Add Transport Configuration to Config

**File**: `pkg/utils/config.go`

Add a new `TransportConfig` struct and embed it in `Config`:

```go
// TransportConfig holds HTTP transport tuning parameters
type TransportConfig struct {
    MaxIdleConns        int    `json:"max_idle_conns" yaml:"max_idle_conns"`
    MaxIdleConnsPerHost int    `json:"max_idle_conns_per_host" yaml:"max_idle_conns_per_host"`
    MaxConnsPerHost     int    `json:"max_conns_per_host" yaml:"max_conns_per_host"`
    IdleConnTimeout     string `json:"idle_conn_timeout" yaml:"idle_conn_timeout"`
    TLSHandshakeTimeout string `json:"tls_handshake_timeout" yaml:"tls_handshake_timeout"`
    ResponseHeaderTimeout string `json:"response_header_timeout" yaml:"response_header_timeout"`
    ExpectContinueTimeout string `json:"expect_continue_timeout" yaml:"expect_continue_timeout"`
    DisableKeepAlives   bool   `json:"disable_keep_alives" yaml:"disable_keep_alives"`
    DisableCompression  bool   `json:"disable_compression" yaml:"disable_compression"`
    WriteBufferSize     int    `json:"write_buffer_size" yaml:"write_buffer_size"`
    ReadBufferSize      int    `json:"read_buffer_size" yaml:"read_buffer_size"`
}
```

Add field to `Config`:
```go
type Config struct {
    // ... existing fields ...
    Transport TransportConfig `json:"transport" yaml:"transport"`
}
```

### Step 2: Add Default Values and Validation

**File**: `pkg/utils/config.go`

Add defaults in `LoadConfig` after unmarshalling:

```go
// Set transport defaults for high throughput
if config.Transport.MaxIdleConns == 0 {
    config.Transport.MaxIdleConns = 1000
}
if config.Transport.MaxIdleConnsPerHost == 0 {
    config.Transport.MaxIdleConnsPerHost = 200
}
if config.Transport.MaxConnsPerHost == 0 {
    config.Transport.MaxConnsPerHost = 500
}
if config.Transport.IdleConnTimeout == "" {
    config.Transport.IdleConnTimeout = "90s"
}
if config.Transport.TLSHandshakeTimeout == "" {
    config.Transport.TLSHandshakeTimeout = "5s"
}
if config.Transport.ResponseHeaderTimeout == "" {
    config.Transport.ResponseHeaderTimeout = "10s"
}
if config.Transport.ExpectContinueTimeout == "" {
    config.Transport.ExpectContinueTimeout = "1s"
}
if config.Transport.WriteBufferSize == 0 {
    config.Transport.WriteBufferSize = 32 * 1024  // 32KB
}
if config.Transport.ReadBufferSize == 0 {
    config.Transport.ReadBufferSize = 32 * 1024  // 32KB
}
```

Add validation in `ValidateConfig`:
```go
// Validate transport config
if config.Transport.MaxIdleConns < 0 {
    errors = append(errors, "transport.max_idle_conns must be non-negative")
}
if config.Transport.MaxIdleConnsPerHost < 0 {
    errors = append(errors, "transport.max_idle_conns_per_host must be non-negative")
}
if config.Transport.MaxConnsPerHost < 0 {
    errors = append(errors, "transport.max_conns_per_host must be non-negative")
}
if config.Transport.IdleConnTimeout != "" {
    if _, err := time.ParseDuration(config.Transport.IdleConnTimeout); err != nil {
        errors = append(errors, fmt.Sprintf("transport.idle_conn_timeout is invalid: %v", err))
    }
}
```

### Step 3: Update HTTPProxy to Use Configurable Transport

**File**: `internal/usecase/proxy.go`

Modify `NewHTTPProxy` to accept transport config:

```go
func NewHTTPProxy(
    cbManager CircuitBreakerUsecase,
    rlManager RateLimiterUsecase,
    lb *entity.LoadBalancer,
    enableSingleflight bool,
    timeout time.Duration,
    transport *utils.TransportConfig,
) *HTTPProxy {
    idleConnTimeout, _ := time.ParseDuration(transport.IdleConnTimeout)
    tlsHandshakeTimeout, _ := time.ParseDuration(transport.TLSHandshakeTimeout)
    responseHeaderTimeout, _ := time.ParseDuration(transport.ResponseHeaderTimeout)
    expectContinueTimeout, _ := time.ParseDuration(transport.ExpectContinueTimeout)

    t := &http.Transport{
        MaxIdleConns:          transport.MaxIdleConns,
        MaxIdleConnsPerHost:   transport.MaxIdleConnsPerHost,
        MaxConnsPerHost:       transport.MaxConnsPerHost,
        IdleConnTimeout:       idleConnTimeout,
        TLSHandshakeTimeout:   tlsHandshakeTimeout,
        ResponseHeaderTimeout: responseHeaderTimeout,
        ExpectContinueTimeout: expectContinueTimeout,
        DisableKeepAlives:     transport.DisableKeepAlives,
        DisableCompression:    transport.DisableCompression,
        WriteBufferSize:       transport.WriteBufferSize,
        ReadBufferSize:        transport.ReadBufferSize,
        ForceAttemptHTTP2:     true,
    }

    return &HTTPProxy{
        cbManager:          cbManager,
        rlManager:          rlManager,
        lb:                 lb,
        enableSingleflight: enableSingleflight,
        httpClient: &http.Client{
            Timeout:   timeout,
            Transport: t,
        },
    }
}
```

### Step 4: Update main.go to Pass Transport Config

**File**: `cmd/main.go`

**FIND** (line ~68):
```go
proxy := usecase.NewHTTPProxy(cbManager, rlManager, lb, config.EnableSingleflight, 30*time.Second)
```

**REPLACE** with:
```go
proxy := usecase.NewHTTPProxy(cbManager, rlManager, lb, config.EnableSingleflight, 30*time.Second, &config.Transport)
```

### Step 5: Update config.json

**File**: `config.json`

Add transport section:
```json
{
  "transport": {
    "max_idle_conns": 1000,
    "max_idle_conns_per_host": 200,
    "max_conns_per_host": 500,
    "idle_conn_timeout": "90s",
    "tls_handshake_timeout": "5s",
    "response_header_timeout": "10s",
    "expect_continue_timeout": "1s",
    "disable_keep_alives": false,
    "disable_compression": false,
    "write_buffer_size": 32768,
    "read_buffer_size": 32768
  }
}
```

### Step 6: Update Tests

**File**: `internal/usecase/proxy_test.go`

Update all `NewHTTPProxy` calls to include transport config:

```go
transport := &utils.TransportConfig{
    MaxIdleConns:        100,
    MaxIdleConnsPerHost: 50,
    MaxConnsPerHost:     100,
    IdleConnTimeout:     "90s",
    TLSHandshakeTimeout: "5s",
    ResponseHeaderTimeout: "10s",
    ExpectContinueTimeout: "1s",
    WriteBufferSize:     32768,
    ReadBufferSize:      32768,
}
proxy := NewHTTPProxy(cbManager, rlManager, lb, true, 30*time.Second, transport)
```

---

## Verification Checklist

- [ ] `TransportConfig` struct added to `pkg/utils/config.go`
- [ ] Default values set for all transport fields
- [ ] Validation added for transport fields
- [ ] `NewHTTPProxy` accepts and uses `TransportConfig`
- [ ] `cmd/main.go` passes transport config
- [ ] `config.json` updated with transport section
- [ ] All tests pass: `go test ./...`
- [ ] No race conditions: `go test -race ./...`

---

## Key Config Values Explained

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `MaxIdleConns` | 1000 | Total idle conns across all hosts for 10K RPS |
| `MaxIdleConnsPerHost` | 200 | Per-backend idle pool; prevents constant connect/disconnect |
| `MaxConnsPerHost` | 500 | Hard cap prevents overwhelming a single backend |
| `WriteBufferSize` | 32KB | Larger buffers reduce syscalls for medium-sized payloads |
| `ReadBufferSize` | 32KB | Matches write buffer for symmetric I/O performance |
| `ForceAttemptHTTP2` | true | HTTP/2 multiplexing reduces connection count |

---

## Success Criteria

1. Connection reuse rate > 95% under load (verify via metrics)
2. No ephemeral port exhaustion at 10K RPS
3. p99 latency reduced by eliminating per-request TCP handshakes
4. All existing tests pass without modification to test logic
