# Task 004: Server Tuning & Timeouts Configuration

**Priority**: P0 (Critical)  
**Assigned To**: Engineer B  
**Estimated Effort**: 2-3 hours  
**Dependencies**: None  

**Files to Modify**:
- `cmd/main.go`
- `pkg/utils/config.go`
- `config.json`

---

## Problem Statement

The current HTTP server uses Go's default settings with no tuning:

```go
// cmd/main.go:97-100
server := &http.Server{
    Addr:    config.ListenAddr,
    Handler: http.DefaultServeMux,
}
```

Go's `net/http` defaults are designed for general-purpose use, not high-throughput proxying:

| Setting | Default | Problem at 10K RPS |
|---------|---------|---------------------|
| `ReadTimeout` | None | Slow clients hold connections open forever |
| `WriteTimeout` | None | Stuck writes consume goroutines indefinitely |
| `ReadHeaderTimeout` | None | Slowloris attack vulnerability |
| `IdleTimeout` | None | Idle keep-alive connections consume memory |
| `MaxHeaderBytes` | 1MB | Oversized headers waste memory per connection |

### Why This Matters for 10K RPS

- **Without timeouts**: A few slow/malicious clients can exhaust goroutine pool
- **Goroutine leak**: Each stuck request = 1 goroutine (~8KB stack) → 10K stuck = 80MB leaked
- **Default ServeMux**: `http.DefaultServeMux` is a global shared resource, not ideal for production

---

## Solution

### Step 1: Add Server Configuration to Config

**File**: `pkg/utils/config.go`

Add a `ServerConfig` struct:

```go
// ServerConfig holds HTTP server tuning parameters
type ServerConfig struct {
    ReadTimeout       string `json:"read_timeout" yaml:"read_timeout"`
    WriteTimeout      string `json:"write_timeout" yaml:"write_timeout"`
    ReadHeaderTimeout string `json:"read_header_timeout" yaml:"read_header_timeout"`
    IdleTimeout       string `json:"idle_timeout" yaml:"idle_timeout"`
    MaxHeaderBytes    int    `json:"max_header_bytes" yaml:"max_header_bytes"`
}
```

Add to `Config`:
```go
type Config struct {
    // ... existing fields ...
    Server ServerConfig `json:"server" yaml:"server"`
}
```

### Step 2: Set Defaults

**File**: `pkg/utils/config.go`

In `LoadConfig`, after unmarshalling:

```go
// Set server defaults optimized for proxy workload
if config.Server.ReadTimeout == "" {
    config.Server.ReadTimeout = "30s"
}
if config.Server.WriteTimeout == "" {
    config.Server.WriteTimeout = "60s"  // Higher than read since we stream responses
}
if config.Server.ReadHeaderTimeout == "" {
    config.Server.ReadHeaderTimeout = "5s"
}
if config.Server.IdleTimeout == "" {
    config.Server.IdleTimeout = "120s"
}
if config.Server.MaxHeaderBytes == 0 {
    config.Server.MaxHeaderBytes = 64 * 1024  // 64KB, not 1MB
}
```

### Step 3: Add Validation

**File**: `pkg/utils/config.go`

In `ValidateConfig`:

```go
// Validate server config
serverTimeouts := map[string]string{
    "server.read_timeout":        config.Server.ReadTimeout,
    "server.write_timeout":       config.Server.WriteTimeout,
    "server.read_header_timeout": config.Server.ReadHeaderTimeout,
    "server.idle_timeout":        config.Server.IdleTimeout,
}
for name, value := range serverTimeouts {
    if value != "" {
        if _, err := time.ParseDuration(value); err != nil {
            errors = append(errors, fmt.Sprintf("%s is invalid: %v", name, err))
        }
    }
}
if config.Server.MaxHeaderBytes < 0 {
    errors = append(errors, "server.max_header_bytes must be non-negative")
}
```

### Step 4: Update Server Initialization in main.go

**File**: `cmd/main.go`

**FIND** (lines 83-100):
```go
// HTTP handlers with panic recovery
http.Handle("/metrics", middleware.PanicRecovery(promhttp.Handler()))

http.Handle("/", middleware.PanicRecovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if len(config.Backends) == 0 {
        http.Error(w, "No backends configured", http.StatusInternalServerError)
        return
    }
    endpoint := r.URL.Path
    if err := proxy.ForwardRequest(w, r, endpoint); err != nil {
        log.Printf("Proxy error: %v", err)
    }
})))

server := &http.Server{
    Addr:    config.ListenAddr,
    Handler: http.DefaultServeMux,
}
```

**REPLACE** with:
```go
// Use a dedicated ServeMux instead of DefaultServeMux
mux := http.NewServeMux()
mux.Handle("/metrics", middleware.PanicRecovery(promhttp.Handler()))

mux.Handle("/", middleware.PanicRecovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if len(config.Backends) == 0 {
        http.Error(w, "No backends configured", http.StatusInternalServerError)
        return
    }
    endpoint := r.URL.Path
    if err := proxy.ForwardRequest(w, r, endpoint); err != nil {
        log.Printf("Proxy error: %v", err)
    }
})))

// Parse server timeouts
readTimeout, _ := time.ParseDuration(config.Server.ReadTimeout)
writeTimeout, _ := time.ParseDuration(config.Server.WriteTimeout)
readHeaderTimeout, _ := time.ParseDuration(config.Server.ReadHeaderTimeout)
idleTimeout, _ := time.ParseDuration(config.Server.IdleTimeout)

server := &http.Server{
    Addr:              config.ListenAddr,
    Handler:           mux,
    ReadTimeout:       readTimeout,
    WriteTimeout:      writeTimeout,
    ReadHeaderTimeout: readHeaderTimeout,
    IdleTimeout:       idleTimeout,
    MaxHeaderBytes:    config.Server.MaxHeaderBytes,
}
```

### Step 5: Update config.json

**File**: `config.json`

```json
{
  "server": {
    "read_timeout": "30s",
    "write_timeout": "60s",
    "read_header_timeout": "5s",
    "idle_timeout": "120s",
    "max_header_bytes": 65536
  }
}
```

---

## Verification Checklist

- [ ] `ServerConfig` struct added to `pkg/utils/config.go`
- [ ] Default values set for all server fields
- [ ] Validation added for all timeout fields
- [ ] `cmd/main.go` uses dedicated `ServeMux` (not `DefaultServeMux`)
- [ ] `cmd/main.go` sets all server timeout fields
- [ ] `config.json` updated with server section
- [ ] All tests pass: `go test ./...`
- [ ] Manual test: Start server, verify timeouts are applied

---

## Key Config Values Explained

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `ReadTimeout` | 30s | Max time to read entire request including body |
| `WriteTimeout` | 60s | Higher than read because we stream backend responses |
| `ReadHeaderTimeout` | 5s | Prevents Slowloris; headers should arrive quickly |
| `IdleTimeout` | 120s | Keep-alive connections reuse; 2 min is generous |
| `MaxHeaderBytes` | 64KB | Reduces per-conn memory; most headers are < 8KB |

---

## Success Criteria

1. Server starts with configured timeouts (verify in startup logs)
2. Slow clients are disconnected after timeout
3. No goroutine leaks under sustained load
4. Uses dedicated ServeMux, not the global `http.DefaultServeMux`
