package repository

import (
	"context"
	"sync"
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
