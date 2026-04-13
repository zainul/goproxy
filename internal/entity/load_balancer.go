package entity

import (
	"math"
	"sync"
	"sync/atomic"
)

type LBStrategy string

const (
	LBStrategyRoundRobin LBStrategy = "round_robin"
	LBStrategyWeightedRR LBStrategy = "weighted_round_robin"
	LBStrategyLeastConn  LBStrategy = "least_connections"
)

type Backend struct {
	URL         string
	IsHealthy   bool
	IsReady     bool
	SuccessRate float64
	Weight      int   // For weighted round-robin (default: 1)
	ActiveConns int64 // Atomic -- current active connections for least-conn
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

type LoadBalancer struct {
	backends []*Backend
	counter  uint64
	mu       sync.RWMutex
	strategy LBStrategy
	// For weighted round-robin
	weights     []int
	totalWeight int
	currentWt   int64 // atomic
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

func (lb *LoadBalancer) NextBackend() *Backend {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	if len(lb.backends) == 0 {
		return nil
	}

	idx := atomic.AddUint64(&lb.counter, 1) % uint64(len(lb.backends))
	return lb.backends[idx]
}

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

func (lb *LoadBalancer) UpdateBackend(url string, isHealthy, isReady bool, successRate float64) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	for _, b := range lb.backends {
		if b.URL == url {
			b.IsHealthy = isHealthy
			b.IsReady = isReady
			b.SuccessRate = successRate
			return
		}
	}
}

func (lb *LoadBalancer) AddBackend(backend *Backend) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if lb.strategy == LBStrategyWeightedRR {
		lb.initWeights()
	}
	lb.backends = append(lb.backends, backend)
}

func (lb *LoadBalancer) GetBackends() []*Backend {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	result := make([]*Backend, len(lb.backends))
	copy(result, lb.backends)
	return result
}
