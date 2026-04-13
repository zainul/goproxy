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
