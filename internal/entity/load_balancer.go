package entity

import (
	"sync"
	"sync/atomic"
)

type Backend struct {
	URL         string
	IsHealthy   bool
	IsReady     bool
	SuccessRate float64
}

type LoadBalancer struct {
	backends []*Backend
	counter  uint64
	mu       sync.RWMutex
}

func NewLoadBalancer(backends []*Backend) *LoadBalancer {
	return &LoadBalancer{
		backends: backends,
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
	lb.backends = append(lb.backends, backend)
}

func (lb *LoadBalancer) GetBackends() []*Backend {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	result := make([]*Backend, len(lb.backends))
	copy(result, lb.backends)
	return result
}
