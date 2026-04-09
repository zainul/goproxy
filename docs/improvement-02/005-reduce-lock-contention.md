# Task 005: Reduce Lock Contention in Circuit Breaker

**Priority**: P1 (High)  
**Assigned To**: Engineer A  
**Estimated Effort**: 3-4 hours  
**Dependencies**: None  

**Files to Modify**:
- `internal/entity/circuit_breaker.go`
- `internal/usecase/circuit_breaker.go`
- `internal/usecase/circuit_breaker_test.go`

---

## Problem Statement

The circuit breaker has several concurrency bottlenecks at high RPS:

### Bottleneck 1: SlidingWindowCounter uses a slice + mutex

```go
// internal/entity/circuit_breaker.go:106-116
func (s *SlidingWindowCounter) Record(success bool) {
    s.mu.Lock()
    defer s.mu.Unlock()
    now := time.Now()
    s.metrics = append(s.metrics, Metric{Timestamp: now, Success: success})
    cutoff := now.Add(-s.window)
    for len(s.metrics) > 0 && s.metrics[0].Timestamp.Before(cutoff) {
        s.metrics = s.metrics[1:]   // O(n) slice shift
    }
}
```

At 10K RPS, every request calls `Record()` which:
- Holds a **write lock** for the entire append + cleanup
- Cleanup is **O(n)** — shifts the entire slice
- Slice `append` triggers re-allocation when capacity is exceeded
- All 10K goroutines contend on the same mutex

### Bottleneck 2: RingBufferCounter has data race potential

```go
// internal/entity/circuit_breaker.go:66-69
func (r *RingBufferCounter) Record(success bool) {
    idx := atomic.AddInt32(&r.index, 1) % int32(r.size)
    atomic.StoreInt32(&r.count, min(atomic.LoadInt32(&r.count)+1, int32(r.size)))
    r.buffer[idx] = success  // NOT atomic — data race!
}
```

The `r.buffer[idx] = success` is a non-atomic write to a shared bool slice. Under concurrent access, this is a data race.

### Bottleneck 3: CircuitBreakerManager RLock per request

```go
// internal/usecase/circuit_breaker.go:52-54
m.mu.RLock()
breaker, exists := m.breakers[backendURL]
m.mu.RUnlock()
```

At 10K RPS, the `RLock/RUnlock` for every request creates cache line bouncing on the mutex even for reads.

---

## Solution

### Step 1: Fix RingBufferCounter Data Race

**File**: `internal/entity/circuit_breaker.go`

Replace `[]bool` with `[]int32` and use atomic operations:

**FIND**:
```go
type RingBufferCounter struct {
    buffer []bool
    size   int
    index  int32
    count  int32
}

func NewRingBufferCounter(size int) *RingBufferCounter {
    return &RingBufferCounter{
        buffer: make([]bool, size),
        size:   size,
    }
}

func (r *RingBufferCounter) Record(success bool) {
    idx := atomic.AddInt32(&r.index, 1) % int32(r.size)
    atomic.StoreInt32(&r.count, min(atomic.LoadInt32(&r.count)+1, int32(r.size)))
    r.buffer[idx] = success
}

func (r *RingBufferCounter) CountFailures() int {
    failures := 0
    count := int(atomic.LoadInt32(&r.count))
    for i := 0; i < count; i++ {
        idx := (int(atomic.LoadInt32(&r.index)) - i + r.size) % r.size
        if !r.buffer[idx] {
            failures++
        }
    }
    return failures
}
```

**REPLACE** with:
```go
type RingBufferCounter struct {
    buffer []int32  // 1 = success, 0 = failure (atomic-safe)
    size   int
    index  int32
    count  int32
}

func NewRingBufferCounter(size int) *RingBufferCounter {
    return &RingBufferCounter{
        buffer: make([]int32, size),
        size:   size,
    }
}

func (r *RingBufferCounter) Record(success bool) {
    idx := atomic.AddInt32(&r.index, 1) % int32(r.size)
    currentCount := atomic.LoadInt32(&r.count)
    if currentCount < int32(r.size) {
        atomic.CompareAndSwapInt32(&r.count, currentCount, currentCount+1)
    }
    val := int32(0)
    if success {
        val = 1
    }
    atomic.StoreInt32(&r.buffer[idx], val)
}

func (r *RingBufferCounter) CountFailures() int {
    failures := 0
    count := int(atomic.LoadInt32(&r.count))
    currentIdx := int(atomic.LoadInt32(&r.index))
    for i := 0; i < count; i++ {
        idx := (currentIdx - i + r.size) % r.size
        if atomic.LoadInt32(&r.buffer[idx]) == 0 {
            failures++
        }
    }
    return failures
}
```

### Step 2: Optimize SlidingWindowCounter

**File**: `internal/entity/circuit_breaker.go`

Replace O(n) cleanup with atomic failure counter:

**FIND**:
```go
type SlidingWindowCounter struct {
    window  time.Duration
    metrics []Metric
    mu      sync.RWMutex
}

func NewSlidingWindowCounter(window time.Duration) *SlidingWindowCounter {
    return &SlidingWindowCounter{
        window:  window,
        metrics: make([]Metric, 0),
    }
}

func (s *SlidingWindowCounter) Record(success bool) {
    s.mu.Lock()
    defer s.mu.Unlock()
    now := time.Now()
    s.metrics = append(s.metrics, Metric{Timestamp: now, Success: success})
    cutoff := now.Add(-s.window)
    for len(s.metrics) > 0 && s.metrics[0].Timestamp.Before(cutoff) {
        s.metrics = s.metrics[1:]
    }
}

func (s *SlidingWindowCounter) FailureRate() float64 {
    s.mu.RLock()
    defer s.mu.RUnlock()
    if len(s.metrics) == 0 {
        return 0.0
    }
    failures := 0
    for _, m := range s.metrics {
        if !m.Success {
            failures++
        }
    }
    return float64(failures) / float64(len(s.metrics))
}
```

**REPLACE** with:
```go
type SlidingWindowCounter struct {
    window      time.Duration
    metrics     []Metric
    mu          sync.Mutex  // Use Mutex, not RWMutex — RWMutex has higher overhead for short critical sections
    totalCount  int64       // atomic
    failCount   int64       // atomic — for fast FailureRate without lock
}

func NewSlidingWindowCounter(window time.Duration) *SlidingWindowCounter {
    return &SlidingWindowCounter{
        window:  window,
        metrics: make([]Metric, 0, 128),  // Pre-allocate to reduce early reallocations
    }
}

func (s *SlidingWindowCounter) Record(success bool) {
    now := time.Now()

    s.mu.Lock()
    s.metrics = append(s.metrics, Metric{Timestamp: now, Success: success})

    // Cleanup expired entries
    cutoff := now.Add(-s.window)
    cutIdx := 0
    for cutIdx < len(s.metrics) && s.metrics[cutIdx].Timestamp.Before(cutoff) {
        cutIdx++
    }
    if cutIdx > 0 {
        // Remove expired entries and count removed failures
        removedFailures := int64(0)
        for i := 0; i < cutIdx; i++ {
            if !s.metrics[i].Success {
                removedFailures++
            }
        }
        s.metrics = s.metrics[cutIdx:]
        atomic.AddInt64(&s.failCount, -removedFailures)
    }
    s.mu.Unlock()

    // Update atomic counters outside the lock
    atomic.AddInt64(&s.totalCount, 1)
    if !success {
        atomic.AddInt64(&s.failCount, 1)
    }
}

func (s *SlidingWindowCounter) FailureRate() float64 {
    total := atomic.LoadInt64(&s.totalCount)
    if total == 0 {
        return 0.0
    }
    failures := atomic.LoadInt64(&s.failCount)
    return float64(failures) / float64(total)
}
```

### Step 3: Use sync.Map for CircuitBreakerManager

**File**: `internal/usecase/circuit_breaker.go`

Replace `map + RWMutex` with `sync.Map` for the read-heavy breaker lookup:

**FIND**:
```go
type CircuitBreakerManager struct {
    breakers map[string]*entity.CircuitBreaker
    mu       sync.RWMutex
}

func NewCircuitBreakerManager() *CircuitBreakerManager {
    return &CircuitBreakerManager{
        breakers: make(map[string]*entity.CircuitBreaker),
    }
}
```

**REPLACE** with:
```go
type CircuitBreakerManager struct {
    breakers sync.Map  // map[string]*entity.CircuitBreaker — optimized for read-heavy workloads
}

func NewCircuitBreakerManager() *CircuitBreakerManager {
    return &CircuitBreakerManager{}
}
```

Then update all methods to use `sync.Map`:

- `AddBreaker`: Replace `m.mu.Lock()` / `m.breakers[url] = cb` with `m.breakers.Store(url, cb)`
- `CanExecute`: Replace `m.mu.RLock()` / `m.breakers[url]` with `m.breakers.Load(url)`
- `RecordSuccess`: Same pattern as CanExecute
- `RecordFailure`: Same pattern as RecordFailure
- `GetState`: Same pattern

**Example — CanExecute**:
```go
func (m *CircuitBreakerManager) CanExecute(backendURL string) (canExecute bool) {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("Panic in CircuitBreakerManager.CanExecute: %v\nStack trace:\n%s", r, debug.Stack())
            canExecute = true
        }
    }()

    val, exists := m.breakers.Load(backendURL)
    if !exists {
        return true
    }
    breaker := val.(*entity.CircuitBreaker)

    switch breaker.State {
    case entity.StateClosed:
        return true
    case entity.StateOpen:
        if time.Since(breaker.LastFailTime) > breaker.Timeout {
            breaker.State = entity.StateHalfOpen
            metrics.CircuitState.WithLabelValues(backendURL).Set(float64(entity.StateHalfOpen))
            return true
        }
        return false
    case entity.StateHalfOpen:
        return true
    default:
        return false
    }
}
```

> **Note**: State transitions on `breaker.State` should use `atomic.StoreInt32` / `atomic.LoadInt32` for the `State` field to avoid races. Update the `State` type to use atomic operations.

### Step 4: Make Circuit Breaker State Atomic

**File**: `internal/entity/circuit_breaker.go`

Update the `CircuitBreaker` struct to use atomic state:

```go
type CircuitBreaker struct {
    state            int32  // atomic — use entity.StateClosed/StateOpen/StateHalfOpen
    FailureThreshold float64
    SuccessThreshold float64
    Timeout          time.Duration
    LastFailTime     time.Time
    lastFailMu       sync.Mutex  // protects LastFailTime only
    CounterType      CounterType
    RingBuffer       *RingBufferCounter
    SlidingWindow    *SlidingWindowCounter
}

// GetState returns the current state atomically
func (cb *CircuitBreaker) GetState() State {
    return State(atomic.LoadInt32(&cb.state))
}

// SetState updates the state atomically
func (cb *CircuitBreaker) SetState(s State) {
    atomic.StoreInt32(&cb.state, int32(s))
}

// SetLastFailTime safely updates the last failure time
func (cb *CircuitBreaker) SetLastFailTime(t time.Time) {
    cb.lastFailMu.Lock()
    cb.LastFailTime = t
    cb.lastFailMu.Unlock()
}
```

### Step 5: Update Tests for Atomic Operations

**File**: `internal/usecase/circuit_breaker_test.go`

Update tests to use `GetState()` and `SetState()` instead of direct field access. Add a concurrent stress test:

```go
func TestCircuitBreaker_ConcurrentRecording(t *testing.T) {
    cbManager := NewCircuitBreakerManager()
    cbManager.AddBreaker("http://test.com", utils.CircuitBreakerConfig{
        FailureThreshold: 50,
        SuccessThreshold: 3,
        Timeout:          "10s",
        CounterType:      "ringbuffer",
        WindowSize:       100,
    })

    var wg sync.WaitGroup
    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            if i%3 == 0 {
                cbManager.RecordFailure("http://test.com")
            } else {
                cbManager.RecordSuccess("http://test.com")
            }
            cbManager.CanExecute("http://test.com")
            cbManager.GetState("http://test.com")
        }(i)
    }
    wg.Wait()
    // Test passes if no race/panic — verify with -race flag
}
```

---

## Verification Checklist

- [ ] `RingBufferCounter.buffer` changed to `[]int32` with atomic operations
- [ ] `SlidingWindowCounter` uses atomic counters for `FailureRate()`
- [ ] `CircuitBreakerManager` uses `sync.Map` for `breakers`
- [ ] `CircuitBreaker.State` uses atomic load/store
- [ ] `CircuitBreaker.LastFailTime` protected by dedicated mutex
- [ ] Concurrent stress test added
- [ ] All tests pass: `go test ./...`
- [ ] No race conditions: `go test -race ./...`

---

## Performance Impact

| Operation | Before | After |
|-----------|--------|-------|
| `CanExecute()` | RWMutex RLock + map lookup | `sync.Map.Load` (lock-free read) |
| `Record()` (RingBuffer) | Data race on `[]bool` | Atomic `StoreInt32` |
| `Record()` (SlidingWindow) | Mutex + O(n) cleanup + alloc | Mutex + binary cleanup + atomic counters |
| `FailureRate()` | RWMutex RLock + iterate all | Two atomic loads (O(1)) |
| `GetState()` | RWMutex RLock | Atomic load (single instruction) |

---

## Important Notes

1. **sync.Map vs RWMutex**: `sync.Map` is optimal when keys are stable and reads dominate writes — exactly our circuit breaker pattern (keys added at startup, reads on every request)
2. **Atomic state**: Circuit breaker state transitions are infrequent (only on threshold breach), so atomic CAS is fine
3. **SlidingWindow still needs mutex for slice operations**, but `FailureRate()` is now lock-free via atomic counters
4. **This task has NO file overlap with Engineer B's tasks** — safe to work in parallel
