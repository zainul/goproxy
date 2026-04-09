# Task 007: Redis Connection Pool Optimization

**Priority**: P1 (High)  
**Assigned To**: Engineer A  
**Estimated Effort**: 2-3 hours  
**Dependencies**: Task 003 (In-Memory Rate Limiter) completes first — they share `cmd/main.go` and config  

**Files to Modify**:
- `cmd/main.go`
- `pkg/utils/config.go`
- `config.json`

---

## Problem Statement

The Redis client is created with minimal configuration:

```go
// cmd/main.go:35-39
rdb := redis.NewClient(&redis.Options{
    Addr:     config.Redis.Addr,
    Password: config.Redis.Password,
    DB:       config.Redis.DB,
})
```

The `go-redis` defaults are:
- `PoolSize`: 10 * runtime.GOMAXPROCS (typically 40-80)
- `MinIdleConns`: 0
- `MaxRetries`: 3
- `DialTimeout`: 5s
- `ReadTimeout`: 3s
- `WriteTimeout`: 3s

For 10K RPS when using Redis-backed rate limiting, these defaults are problematic:
- **Pool size too small**: 10K RPS with 0.5ms avg Redis latency = ~5 concurrent connections needed, but bursts need 10-20x headroom
- **No minimum idle connections**: Cold starts cause latency spikes
- **No connection max lifetime**: Stale connections may silently break

---

## Solution

### Step 1: Extend RedisConfig

**File**: `pkg/utils/config.go`

**FIND**:
```go
type RedisConfig struct {
    Addr     string `json:"addr" yaml:"addr"`
    Password string `json:"password" yaml:"password"`
    DB       int    `json:"db" yaml:"db"`
}
```

**REPLACE** with:
```go
type RedisConfig struct {
    Addr            string `json:"addr" yaml:"addr"`
    Password        string `json:"password" yaml:"password"`
    DB              int    `json:"db" yaml:"db"`
    PoolSize        int    `json:"pool_size" yaml:"pool_size"`
    MinIdleConns    int    `json:"min_idle_conns" yaml:"min_idle_conns"`
    MaxRetries      int    `json:"max_retries" yaml:"max_retries"`
    DialTimeout     string `json:"dial_timeout" yaml:"dial_timeout"`
    ReadTimeout     string `json:"read_timeout" yaml:"read_timeout"`
    WriteTimeout    string `json:"write_timeout" yaml:"write_timeout"`
    PoolTimeout     string `json:"pool_timeout" yaml:"pool_timeout"`
    ConnMaxLifetime string `json:"conn_max_lifetime" yaml:"conn_max_lifetime"`
    ConnMaxIdleTime string `json:"conn_max_idle_time" yaml:"conn_max_idle_time"`
}
```

### Step 2: Add Defaults

**File**: `pkg/utils/config.go`

In `LoadConfig`, after unmarshalling:

```go
// Set Redis pool defaults for high throughput
if config.Redis.PoolSize == 0 {
    config.Redis.PoolSize = 200
}
if config.Redis.MinIdleConns == 0 {
    config.Redis.MinIdleConns = 20
}
if config.Redis.MaxRetries == 0 {
    config.Redis.MaxRetries = 3
}
if config.Redis.DialTimeout == "" {
    config.Redis.DialTimeout = "5s"
}
if config.Redis.ReadTimeout == "" {
    config.Redis.ReadTimeout = "2s"
}
if config.Redis.WriteTimeout == "" {
    config.Redis.WriteTimeout = "2s"
}
if config.Redis.PoolTimeout == "" {
    config.Redis.PoolTimeout = "3s"
}
if config.Redis.ConnMaxLifetime == "" {
    config.Redis.ConnMaxLifetime = "30m"
}
if config.Redis.ConnMaxIdleTime == "" {
    config.Redis.ConnMaxIdleTime = "5m"
}
```

### Step 3: Add Validation

**File**: `pkg/utils/config.go`

In `ValidateConfig`:

```go
// Validate Redis pool config
if config.Redis.PoolSize < 0 {
    errors = append(errors, "redis.pool_size must be non-negative")
}
if config.Redis.MinIdleConns < 0 {
    errors = append(errors, "redis.min_idle_conns must be non-negative")
}
if config.Redis.MinIdleConns > config.Redis.PoolSize && config.Redis.PoolSize > 0 {
    errors = append(errors, "redis.min_idle_conns must not exceed redis.pool_size")
}
redisTimeouts := map[string]string{
    "redis.dial_timeout":      config.Redis.DialTimeout,
    "redis.read_timeout":      config.Redis.ReadTimeout,
    "redis.write_timeout":     config.Redis.WriteTimeout,
    "redis.pool_timeout":      config.Redis.PoolTimeout,
    "redis.conn_max_lifetime": config.Redis.ConnMaxLifetime,
    "redis.conn_max_idle_time": config.Redis.ConnMaxIdleTime,
}
for name, value := range redisTimeouts {
    if value != "" {
        if _, err := time.ParseDuration(value); err != nil {
            errors = append(errors, fmt.Sprintf("%s is invalid: %v", name, err))
        }
    }
}
```

### Step 4: Update Redis Client Initialization

**File**: `cmd/main.go`

**FIND** (lines ~35-39):
```go
rdb := redis.NewClient(&redis.Options{
    Addr:     config.Redis.Addr,
    Password: config.Redis.Password,
    DB:       config.Redis.DB,
})
```

**REPLACE** with:
```go
// Parse Redis timeouts
redisDialTimeout, _ := time.ParseDuration(config.Redis.DialTimeout)
redisReadTimeout, _ := time.ParseDuration(config.Redis.ReadTimeout)
redisWriteTimeout, _ := time.ParseDuration(config.Redis.WriteTimeout)
redisPoolTimeout, _ := time.ParseDuration(config.Redis.PoolTimeout)
redisConnMaxLifetime, _ := time.ParseDuration(config.Redis.ConnMaxLifetime)
redisConnMaxIdleTime, _ := time.ParseDuration(config.Redis.ConnMaxIdleTime)

rdb := redis.NewClient(&redis.Options{
    Addr:            config.Redis.Addr,
    Password:        config.Redis.Password,
    DB:              config.Redis.DB,
    PoolSize:        config.Redis.PoolSize,
    MinIdleConns:    config.Redis.MinIdleConns,
    MaxRetries:      config.Redis.MaxRetries,
    DialTimeout:     redisDialTimeout,
    ReadTimeout:     redisReadTimeout,
    WriteTimeout:    redisWriteTimeout,
    PoolTimeout:     redisPoolTimeout,
    ConnMaxLifetime: redisConnMaxLifetime,
    ConnMaxIdleTime: redisConnMaxIdleTime,
})

log.Printf("Redis pool configured: size=%d, min_idle=%d, addr=%s",
    config.Redis.PoolSize, config.Redis.MinIdleConns, config.Redis.Addr)
```

### Step 5: Update config.json

```json
{
  "redis": {
    "addr": "localhost:6379",
    "password": "",
    "db": 0,
    "pool_size": 200,
    "min_idle_conns": 20,
    "max_retries": 3,
    "dial_timeout": "5s",
    "read_timeout": "2s",
    "write_timeout": "2s",
    "pool_timeout": "3s",
    "conn_max_lifetime": "30m",
    "conn_max_idle_time": "5m"
  }
}
```

---

## Verification Checklist

- [ ] `RedisConfig` extended with pool parameters
- [ ] Default values set for all pool parameters
- [ ] Validation added for pool parameters and timeouts
- [ ] `cmd/main.go` passes all pool parameters to `redis.NewClient`
- [ ] Redis pool size logged at startup
- [ ] `config.json` updated with pool parameters
- [ ] All tests pass: `go test ./...`

---

## Key Config Values Explained

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `PoolSize` | 200 | 10K RPS / 0.5ms RTT = ~5 conns, but bursts need 40x headroom |
| `MinIdleConns` | 20 | Always warm; prevents cold-start latency |
| `ReadTimeout` | 2s | Shorter than default 3s; fail fast rather than block goroutine |
| `PoolTimeout` | 3s | Max wait time to get conn from pool; prevents goroutine pile-up |
| `ConnMaxLifetime` | 30m | Force reconnection to distribute across Redis replicas |
| `ConnMaxIdleTime` | 5m | Reclaim unused connections during low traffic |

---

## Important Notes

1. **Only relevant when using Redis rate limiter** (`rate_limiter_storage: "redis"`)
2. **Task 003 adds `rate_limiter_storage` config** — make sure both changes to `Config` struct and `config.json` are merged correctly
3. **Redis connection is still created** even in memory mode (for future features). The pool settings ensure it doesn't waste resources with `MinIdleConns: 0` when unused
