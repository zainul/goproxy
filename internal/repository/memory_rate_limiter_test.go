package repository

import (
	"context"
	"sync"
	"sync/atomic"
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
