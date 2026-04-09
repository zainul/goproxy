# Task 009: Load Balancer Algorithm Upgrade (Weighted Round-Robin + Least Connections)

**Priority**: P2 (Medium)  
**Assigned To**: Engineer A  
**Estimated Effort**: 3-4 hours  
**Dependencies**: Task 005 (Lock Contention) completes first — both touch `internal/entity/`  

**Files to Modify**:
- `internal/entity/load_balancer.go`
- `pkg/utils/config.go`
- `cmd/main.go`
- `config.json`

**Files to Create**:
- `internal/entity/load_balancer_test.go`

---

## Problem Statement

The current load balancer uses simple round-robin:

```go
// internal/entity/load_balancer.go:48-56
for i := uint64(0); i < count; i++ {
    idx := atomic.AddUint64(&lb.counter, 1) % count
    backend := lb.backends[idx]
    if backend.IsHealthy && backend.IsReady {
        return backend
    }
}
```

At 10K RPS, round-robin has issues:
1. **Ignores backend capacity**: A 4-core server gets the same traffic as a 32-core server
2. **Ignores current load**: A backend processing 500 concurrent requests gets new requests same as one processing 10
3. **No weight support**: Cannot express "send 80% to primary, 20% to canary"

### Impact at 10K RPS

If 3 backends have different capacities:
- Backend A: handles 5000 RPS
- Backend B: handles 3000 RPS  
- Backend C: handles 2000 RPS

Round-robin sends ~3333 to each, overloading C and underutilizing A.

---

## Solution: Pluggable Load Balancing with Weighted Round-Robin + Least Connections

### Step 1: Define Load Balancer Strategy Interface

**File**: `internal/entity/load_balancer.go`

Add strategy types and an algorithm interface:

```go
// LBStrategy represents the load balancing algorithm to use
type LBStrategy string

const (
    LBStrategyRoundRobin    LBStrategy = "round_robin"
    LBStrategyWeightedRR    LBStrategy = "weighted_round_robin"
    LBStrategyLeastConn     LBStrategy = "least_connections"
)
```

### Step 2: Add Weight and Active Connections to Backend

**File**: `internal/entity/load_balancer.go`

Extend the `Backend` struct:

```go
type Backend struct {
    URL             string
    IsHealthy       bool
    IsReady         bool
    SuccessRate     float64
    Weight          int     // For weighted round-robin (default: 1)
    ActiveConns     int64   // Atomic — current active connections for least-conn
}

// IncrementConns atomically increments active connections
func (b *Backend) IncrementConns() {
    atomic.AddInt64(&b.ActiveConns, 1)
}

// DecrementConns atomically decrements active connections
func (b *Backend) DecrementConns() {
    atomic.AddInt64(&b.ActiveConns, -1)
}

// GetActiveConns returns the current active connection count
func (b *Backend) GetActiveConns() int64 {
    return atomic.LoadInt64(&b.ActiveConns)
}
```

### Step 3: Add Strategy Field to LoadBalancer

**File**: `internal/entity/load_balancer.go`

```go
type LoadBalancer struct {
    backends  []*Backend
    counter   uint64
    mu        sync.RWMutex
    strategy  LBStrategy
    // For weighted round-robin
    weights      []int
    totalWeight  int
    currentWt    int64  // atomic
}

func NewLoadBalancer(backends []*Backend, strategy LBStrategy) *LoadBalancer {
    lb := &LoadBalancer{
        backends: backends,
        strategy: strategy,
    }
    if strategy == LBStrategyWeightedRR {
        lb.initWeights()
    }
    return lb
}

func (lb *LoadBalancer) initWeights() {
    lb.weights = make([]int, len(lb.backends))
    lb.totalWeight = 0
    for i, b := range lb.backends {
        w := b.Weight
        if w <= 0 {
            w = 1
        }
        lb.weights[i] = w
        lb.totalWeight += w
    }
}
```

### Step 4: Implement Weighted Round-Robin

**File**: `internal/entity/load_balancer.go`

```go
func (lb *LoadBalancer) nextWeightedRR() *Backend {
    lb.mu.RLock()
    defer lb.mu.RUnlock()

    if len(lb.backends) == 0 {
        return nil
    }

    pos := atomic.AddInt64(&lb.currentWt, 1)
    target := int(pos % int64(lb.totalWeight))

    sum := 0
    for i, b := range lb.backends {
        sum += lb.weights[i]
        if target < sum {
            if b.IsHealthy && b.IsReady {
                return b
            }
            // If selected backend is unhealthy, fall through to next
            break
        }
    }

    // Fallback: find any healthy backend
    for _, b := range lb.backends {
        if b.IsHealthy && b.IsReady {
            return b
        }
    }

    // Last resort: return first backend
    return lb.backends[0]
}
```

### Step 5: Implement Least Connections

**File**: `internal/entity/load_balancer.go`

```go
func (lb *LoadBalancer) nextLeastConn() *Backend {
    lb.mu.RLock()
    defer lb.mu.RUnlock()

    if len(lb.backends) == 0 {
        return nil
    }

    var best *Backend
    bestConns := int64(math.MaxInt64)

    for _, b := range lb.backends {
        if !b.IsHealthy || !b.IsReady {
            continue
        }
        conns := b.GetActiveConns()
        if conns < bestConns {
            bestConns = conns
            best = b
        }
    }

    if best == nil {
        return lb.backends[0]
    }
    return best
}
```

### Step 6: Update NextHealthyBackend to Use Strategy

**File**: `internal/entity/load_balancer.go`

**FIND**:
```go
func (lb *LoadBalancer) NextHealthyBackend() *Backend {
```

Update the method to dispatch based on strategy:

```go
func (lb *LoadBalancer) NextHealthyBackend() *Backend {
    switch lb.strategy {
    case LBStrategyWeightedRR:
        return lb.nextWeightedRR()
    case LBStrategyLeastConn:
        return lb.nextLeastConn()
    default:
        return lb.nextRoundRobin()
    }
}

// nextRoundRobin is the existing round-robin implementation
func (lb *LoadBalancer) nextRoundRobin() *Backend {
    lb.mu.RLock()
    defer lb.mu.RUnlock()

    if len(lb.backends) == 0 {
        return nil
    }

    count := uint64(len(lb.backends))
    for i := uint64(0); i < count; i++ {
        idx := atomic.AddUint64(&lb.counter, 1) % count
        backend := lb.backends[idx]
        if backend.IsHealthy && backend.IsReady {
            return backend
        }
    }

    return lb.backends[atomic.AddUint64(&lb.counter, 1)%count]
}
```

### Step 7: Track Active Connections in Proxy

**File**: `internal/usecase/proxy.go`

In `ForwardRequest`, after selecting the backend:

```go
backend := p.lb.NextHealthyBackend()
if backend == nil {
    // ... error handling ...
}
backendURL := backend.URL

// Track active connections for least-conn
backend.IncrementConns()
defer backend.DecrementConns()
```

### Step 8: Add Config

**File**: `pkg/utils/config.go`

Add to `Config`:
```go
type Config struct {
    // ... existing fields ...
    LoadBalancerStrategy string `json:"load_balancer_strategy" yaml:"load_balancer_strategy"`
}
```

Add to `BackendConfig`:
```go
type BackendConfig struct {
    // ... existing fields ...
    Weight int `json:"weight" yaml:"weight"`
}
```

Add defaults:
```go
if config.LoadBalancerStrategy == "" {
    config.LoadBalancerStrategy = "round_robin"
}
```

Add validation:
```go
validStrategies := map[string]bool{
    "round_robin": true, "weighted_round_robin": true, "least_connections": true,
}
if !validStrategies[config.LoadBalancerStrategy] {
    errors = append(errors, "load_balancer_strategy must be 'round_robin', 'weighted_round_robin', or 'least_connections'")
}
```

### Step 9: Update main.go

**File**: `cmd/main.go`

**FIND**:
```go
lb := entity.NewLoadBalancer(backends)
```

**REPLACE** with:
```go
lb := entity.NewLoadBalancer(backends, entity.LBStrategy(config.LoadBalancerStrategy))
log.Printf("Load balancer strategy: %s", config.LoadBalancerStrategy)
```

Add weight from config:
```go
for _, backendConfig := range config.Backends {
    weight := backendConfig.Weight
    if weight <= 0 {
        weight = 1
    }
    backends = append(backends, &entity.Backend{
        URL:         backendConfig.URL,
        IsHealthy:   true,
        IsReady:     true,
        SuccessRate: 1.0,
        Weight:      weight,
    })
}
```

### Step 10: Create Tests

**File**: `internal/entity/load_balancer_test.go` (CREATE NEW FILE)

```go
package entity

import (
    "sync"
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestLoadBalancer_RoundRobin(t *testing.T) {
    backends := []*Backend{
        {URL: "http://a.com", IsHealthy: true, IsReady: true, Weight: 1},
        {URL: "http://b.com", IsHealthy: true, IsReady: true, Weight: 1},
        {URL: "http://c.com", IsHealthy: true, IsReady: true, Weight: 1},
    }
    lb := NewLoadBalancer(backends, LBStrategyRoundRobin)

    counts := map[string]int{}
    for i := 0; i < 300; i++ {
        b := lb.NextHealthyBackend()
        counts[b.URL]++
    }
    assert.Equal(t, 100, counts["http://a.com"])
    assert.Equal(t, 100, counts["http://b.com"])
    assert.Equal(t, 100, counts["http://c.com"])
}

func TestLoadBalancer_WeightedRoundRobin(t *testing.T) {
    backends := []*Backend{
        {URL: "http://a.com", IsHealthy: true, IsReady: true, Weight: 5},
        {URL: "http://b.com", IsHealthy: true, IsReady: true, Weight: 3},
        {URL: "http://c.com", IsHealthy: true, IsReady: true, Weight: 2},
    }
    lb := NewLoadBalancer(backends, LBStrategyWeightedRR)

    counts := map[string]int{}
    total := 1000
    for i := 0; i < total; i++ {
        b := lb.NextHealthyBackend()
        counts[b.URL]++
    }
    // Expect approximately 50%, 30%, 20% distribution
    assert.InDelta(t, 500, counts["http://a.com"], 50)
    assert.InDelta(t, 300, counts["http://b.com"], 50)
    assert.InDelta(t, 200, counts["http://c.com"], 50)
}

func TestLoadBalancer_LeastConnections(t *testing.T) {
    backends := []*Backend{
        {URL: "http://a.com", IsHealthy: true, IsReady: true, Weight: 1},
        {URL: "http://b.com", IsHealthy: true, IsReady: true, Weight: 1},
    }
    lb := NewLoadBalancer(backends, LBStrategyLeastConn)

    // Simulate a.com being busy
    backends[0].IncrementConns()
    backends[0].IncrementConns()
    backends[0].IncrementConns()

    // Should prefer b.com
    b := lb.NextHealthyBackend()
    assert.Equal(t, "http://b.com", b.URL)
}

func TestLoadBalancer_ConcurrentAccess(t *testing.T) {
    backends := []*Backend{
        {URL: "http://a.com", IsHealthy: true, IsReady: true, Weight: 1},
        {URL: "http://b.com", IsHealthy: true, IsReady: true, Weight: 1},
    }
    lb := NewLoadBalancer(backends, LBStrategyRoundRobin)

    var wg sync.WaitGroup
    for i := 0; i < 10000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            b := lb.NextHealthyBackend()
            assert.NotNil(t, b)
        }()
    }
    wg.Wait()
}
```

### Step 11: Update config.json

```json
{
  "load_balancer_strategy": "round_robin",
  "backends": [
    {
      "url": "http://httpbin.org",
      "weight": 1,
      ...
    }
  ]
}
```

---

## Verification Checklist

- [ ] `LBStrategy` type and constants defined
- [ ] `Backend` struct extended with `Weight` and `ActiveConns`
- [ ] Weighted round-robin algorithm implemented and tested
- [ ] Least connections algorithm implemented and tested
- [ ] `NextHealthyBackend` dispatches based on strategy
- [ ] Active connections tracked in proxy `ForwardRequest`
- [ ] Config supports `load_balancer_strategy` and `weight`
- [ ] Concurrent access test passes with -race flag
- [ ] All existing tests pass: `go test ./...`

---

## Success Criteria

1. Round-robin distributes evenly across backends
2. Weighted RR respects weight ratios (±5% tolerance)
3. Least-connections selects the backend with fewest active connections
4. Thread-safe under 10K concurrent goroutines
5. No regression in existing load balancer behavior
