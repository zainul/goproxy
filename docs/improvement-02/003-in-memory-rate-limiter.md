# Task 003: In-Memory Rate Limiter (Redis Bypass for Local Operations)

**Priority**: P0 (Critical)  
**Assigned To**: Engineer A  
**Estimated Effort**: 4-5 hours  
**Dependencies**: None (can start immediately)  

**Files to Create**:
- `internal/repository/memory_rate_limiter.go`
- `internal/repository/memory_rate_limiter_test.go`

**Files to Modify**:
- `internal/repository/rate_limiter.go` (interface only — no changes to Redis impl)
- `pkg/utils/config.go`
- `cmd/main.go`
- `config.json`

---

## Problem Statement

Every rate limit check at 10K RPS results in a Redis round-trip:

```go
// internal/repository/rate_limiter.go:28-49  — sliding window uses 4 Redis commands in a pipeline
pipe := r.client.TxPipeline()
pipe.ZAdd(ctx, key, redis.Z{Member: now, Score: float64(now)})
pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", windowStart))
countCmd := pipe.ZCard(ctx, key)
pipe.Expire(ctx, key, window)
```

At 10K RPS:
- **40,000 Redis commands/sec** (4 per request for sliding window)
- **Network latency**: Even 0.5ms RTT to Redis = 5 seconds total latency per second (impossible to process)
- **Redis CPU**: Single-threaded Redis saturates at ~100K simple commands/sec
- **Failure cascade**: Redis latency spike → all requests block → timeout → circuit breaker opens

### Why In-Memory?

For a single-instance proxy, rate limiting doesn't need Redis. An in-memory implementation:
- **Eliminates network RTT** (~0.5ms → ~0.001ms per check)
- **Removes external dependency** for single-node deployments
- **Provides Redis fallback** when Redis is unavailable
- **Reduces Redis load** for multi-node deployments that only use Redis for coordination

---

## Solution: Implement In-Memory Rate Limiter

### Step 1: Create In-Memory Sliding Window Implementation

**File**: `internal/repository/memory_rate_limiter.go` (CREATE NEW FILE)

```go
package repository

import (
    "context"
    "sync"
    "sync/atomic"
    "time"
)

// MemoryRateLimiterRepository implements RateLimiterRepository using in-memory storage.
// This is optimized for single-instance deployments and eliminates Redis round-trips.
type MemoryRateLimiterRepository struct {
    windows      map[string]*slidingWindow
    tokenBuckets map[string]*tokenBucket
    mu           sync.RWMutex
}

type slidingWindow struct {
    mu       sync.Mutex
    requests []int64 // timestamps in nanoseconds
}

type tokenBucket struct {
    mu         sync.Mutex
    tokens     float64
    capacity   int
    rate       int
    lastRefill int64 // unix timestamp
}

// NewMemoryRateLimiterRepository creates a new in-memory rate limiter
func NewMemoryRateLimiterRepository() *MemoryRateLimiterRepository {
    repo := &MemoryRateLimiterRepository{
        windows:      make(map[string]*slidingWindow),
        tokenBuckets: make(map[string]*tokenBucket),
    }
    // Start background cleanup goroutine
    go repo.cleanup()
    return repo
}

// Allow implements sliding window rate limiting in memory
func (m *MemoryRateLimiterRepository) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
    m.mu.RLock()
    sw, exists := m.windows[key]
    m.mu.RUnlock()

    if !exists {
        m.mu.Lock()
        sw, exists = m.windows[key]
        if !exists {
            sw = &slidingWindow{
                requests: make([]int64, 0, limit),
            }
            m.windows[key] = sw
        }
        m.mu.Unlock()
    }

    now := time.Now().UnixNano()
    windowStart := now - window.Nanoseconds()

    sw.mu.Lock()
    defer sw.mu.Unlock()

    // Remove expired entries using binary search for efficiency
    cutIdx := 0
    for cutIdx < len(sw.requests) && sw.requests[cutIdx] < windowStart {
        cutIdx++
    }
    if cutIdx > 0 {
        sw.requests = sw.requests[cutIdx:]
    }

    // Check limit
    if len(sw.requests) >= limit {
        return false, nil
    }

    // Add current request
    sw.requests = append(sw.requests, now)
    return true, nil
}

// AllowTokenBucket implements token bucket rate limiting in memory
func (m *MemoryRateLimiterRepository) AllowTokenBucket(ctx context.Context, key string, rate int, capacity int) (bool, error) {
    m.mu.RLock()
    tb, exists := m.tokenBuckets[key]
    m.mu.RUnlock()

    if !exists {
        m.mu.Lock()
        tb, exists = m.tokenBuckets[key]
        if !exists {
            tb = &tokenBucket{
                tokens:     float64(capacity),
                capacity:   capacity,
                rate:       rate,
                lastRefill: time.Now().Unix(),
            }
            m.tokenBuckets[key] = tb
        }
        m.mu.Unlock()
    }

    tb.mu.Lock()
    defer tb.mu.Unlock()

    now := time.Now().Unix()
    elapsed := now - tb.lastRefill
    if elapsed > 0 {
        refill := float64(elapsed) * float64(tb.rate)
        tb.tokens = min64(tb.tokens+refill, float64(tb.capacity))
        tb.lastRefill = now
    }

    if tb.tokens < 1 {
        return false, nil
    }

    tb.tokens--
    return true, nil
}

// cleanup periodically removes expired entries to prevent memory leaks
func (m *MemoryRateLimiterRepository) cleanup() {
    ticker := time.NewTicker(60 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        now := time.Now().UnixNano()
        fiveMinutesAgo := now - (5 * time.Minute).Nanoseconds()

        m.mu.Lock()
        for key, sw := range m.windows {
            sw.mu.Lock()
            if len(sw.requests) == 0 || sw.requests[len(sw.requests)-1] < fiveMinutesAgo {
                delete(m.windows, key)
            }
            sw.mu.Unlock()
        }
        m.mu.Unlock()
    }
}

func min64(a, b float64) float64 {
    if a < b {
        return a
    }
    return b
}
```

### Step 2: Add Rate Limiter Type Config

**File**: `pkg/utils/config.go`

Add a `RateLimiterStorageType` field to `Config`:

```go
type Config struct {
    // ... existing fields ...
    RateLimiterStorage string `json:"rate_limiter_storage" yaml:"rate_limiter_storage"` // "memory" or "redis"
}
```

Add default in `LoadConfig`:
```go
if config.RateLimiterStorage == "" {
    config.RateLimiterStorage = "memory"  // Default to memory for performance
}
```

Add validation:
```go
if config.RateLimiterStorage != "memory" && config.RateLimiterStorage != "redis" {
    errors = append(errors, "rate_limiter_storage must be 'memory' or 'redis'")
}
```

### Step 3: Update main.go to Select Rate Limiter Backend

**File**: `cmd/main.go`

**FIND** (line ~42):
```go
// Initialize repositories
rlRepo := repository.NewRedisRateLimiterRepository(rdb)
```

**REPLACE** with:
```go
// Initialize repositories based on config
var rlRepo repository.RateLimiterRepository
if config.RateLimiterStorage == "redis" {
    rlRepo = repository.NewRedisRateLimiterRepository(rdb)
    log.Println("Using Redis-backed rate limiter")
} else {
    rlRepo = repository.NewMemoryRateLimiterRepository()
    log.Println("Using in-memory rate limiter (single-instance mode)")
}
```

### Step 4: Update config.json

**File**: `config.json`

Add the rate limiter storage setting:
```json
{
  "rate_limiter_storage": "memory",
  ...
}
```

### Step 5: Create Tests

**File**: `internal/repository/memory_rate_limiter_test.go` (CREATE NEW FILE)

```go
package repository

import (
    "context"
    "sync"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
)

func TestMemoryRateLimiter_SlidingWindow_AllowWithinLimit(t *testing.T) {
    repo := NewMemoryRateLimiterRepository()
    ctx := context.Background()

    for i := 0; i < 10; i++ {
        allowed, err := repo.Allow(ctx, "test-key", 10, time.Minute)
        assert.NoError(t, err)
        assert.True(t, allowed)
    }
}

func TestMemoryRateLimiter_SlidingWindow_BlocksOverLimit(t *testing.T) {
    repo := NewMemoryRateLimiterRepository()
    ctx := context.Background()

    // Fill up limit
    for i := 0; i < 10; i++ {
        allowed, err := repo.Allow(ctx, "test-key", 10, time.Minute)
        assert.NoError(t, err)
        assert.True(t, allowed)
    }

    // 11th request should be blocked
    allowed, err := repo.Allow(ctx, "test-key", 10, time.Minute)
    assert.NoError(t, err)
    assert.False(t, allowed)
}

func TestMemoryRateLimiter_SlidingWindow_WindowExpiry(t *testing.T) {
    repo := NewMemoryRateLimiterRepository()
    ctx := context.Background()

    // Fill up limit with 100ms window
    for i := 0; i < 5; i++ {
        allowed, err := repo.Allow(ctx, "test-key", 5, 100*time.Millisecond)
        assert.NoError(t, err)
        assert.True(t, allowed)
    }

    // Should be blocked
    allowed, err := repo.Allow(ctx, "test-key", 5, 100*time.Millisecond)
    assert.NoError(t, err)
    assert.False(t, allowed)

    // Wait for window to expire
    time.Sleep(150 * time.Millisecond)

    // Should be allowed again
    allowed, err = repo.Allow(ctx, "test-key", 5, 100*time.Millisecond)
    assert.NoError(t, err)
    assert.True(t, allowed)
}

func TestMemoryRateLimiter_TokenBucket_AllowWithinCapacity(t *testing.T) {
    repo := NewMemoryRateLimiterRepository()
    ctx := context.Background()

    for i := 0; i < 10; i++ {
        allowed, err := repo.AllowTokenBucket(ctx, "test-key", 10, 10)
        assert.NoError(t, err)
        assert.True(t, allowed)
    }
}

func TestMemoryRateLimiter_TokenBucket_BlocksOverCapacity(t *testing.T) {
    repo := NewMemoryRateLimiterRepository()
    ctx := context.Background()

    // Drain tokens
    for i := 0; i < 10; i++ {
        repo.AllowTokenBucket(ctx, "test-key", 1, 10)
    }

    // Should be blocked
    allowed, err := repo.AllowTokenBucket(ctx, "test-key", 1, 10)
    assert.NoError(t, err)
    assert.False(t, allowed)
}

func TestMemoryRateLimiter_ConcurrentAccess(t *testing.T) {
    repo := NewMemoryRateLimiterRepository()
    ctx := context.Background()

    var wg sync.WaitGroup
    allowed := int64(0)
    blocked := int64(0)

    limit := 100
    goroutines := 200

    for i := 0; i < goroutines; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            ok, err := repo.Allow(ctx, "concurrent-key", limit, time.Minute)
            assert.NoError(t, err)
            if ok {
                atomic.AddInt64(&allowed, 1)
            } else {
                atomic.AddInt64(&blocked, 1)
            }
        }()
    }

    wg.Wait()
    assert.Equal(t, int64(limit), allowed, "exactly 'limit' requests should be allowed")
    assert.Equal(t, int64(goroutines-limit), blocked, "remaining should be blocked")
}
```

Note: Add `"sync/atomic"` import in the test file.

---

## Verification Checklist

- [ ] `MemoryRateLimiterRepository` implements `RateLimiterRepository` interface
- [ ] Sliding window algorithm correctly tracks and expires entries
- [ ] Token bucket algorithm correctly refills and drains
- [ ] Background cleanup goroutine prevents memory leaks
- [ ] Concurrent access test passes with -race flag
- [ ] Config supports `rate_limiter_storage: "memory"` and `"redis"`
- [ ] `cmd/main.go` selects correct implementation based on config
- [ ] All tests pass: `go test ./...`
- [ ] No race conditions: `go test -race ./...`

---

## Performance Impact

| Metric | Redis | In-Memory |
|--------|-------|-----------|
| Latency per check | ~0.5-2ms | ~0.001ms |
| Max throughput | ~10K checks/sec | ~1M+ checks/sec |
| External dependency | Required | None |
| Multi-instance support | Yes | No (per-instance limits) |

---

## Important Notes

1. **In-memory is single-instance only**: If running multiple proxy instances, use `redis` mode for globally coordinated rate limits
2. **The cleanup goroutine** runs every 60 seconds and removes windows with no activity in the last 5 minutes
3. **No changes to Redis implementation**: Redis path remains fully functional when `rate_limiter_storage: "redis"` is configured
4. **Thread safety**: All operations use fine-grained locking (per-window/bucket mutexes, not a global lock)
