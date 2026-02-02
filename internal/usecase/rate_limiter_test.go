package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"goproxy/pkg/constants"
	"goproxy/pkg/utils"
)

// MockRateLimiterRepository is a mock implementation
type MockRateLimiterRepository struct {
	mock.Mock
}

func (m *MockRateLimiterRepository) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	args := m.Called(ctx, key, limit, window)
	return args.Bool(0), args.Error(1)
}

func (m *MockRateLimiterRepository) AllowTokenBucket(ctx context.Context, key string, rate int, capacity int) (bool, error) {
	args := m.Called(ctx, key, rate, capacity)
	return args.Bool(0), args.Error(1)
}

func TestRateLimiterManager_Allow_SlidingWindow(t *testing.T) {
	t.Logf("Scenario: Rate Limiter Allow with Sliding Window")
	t.Logf("Input: backend='http://backend', config={Type: 'sliding_window', Limit: 100, Window: 60}")

	mockRepo := &MockRateLimiterRepository{}
	manager := NewRateLimiterManager(mockRepo)

	config := utils.RateLimiterConfig{
		Type:   constants.RateLimiterTypeSlidingWindow,
		Limit:  100,
		Window: 60,
	}
	manager.AddLimiter("http://backend", config)

	ctx := context.Background()
	mockRepo.On("Allow", ctx, constants.RateLimitPrefix+"http://backend", 100, time.Minute).Return(true, nil)

	t.Logf("Action: Allow(ctx, 'http://backend')")
	allowed, err := manager.Allow(ctx, "http://backend")
	t.Logf("Output: allowed=%t, err=%v", allowed, err)

	assert.NoError(t, err)
	t.Logf("Assertion: err == nil - PASSED")

	assert.True(t, allowed)
	t.Logf("Assertion: allowed == true - PASSED")

	mockRepo.AssertExpectations(t)
}

func TestRateLimiterManager_Allow_TokenBucket(t *testing.T) {
	t.Logf("Scenario: Rate Limiter Allow with Token Bucket")
	t.Logf("Input: backend='http://backend', config={Type: 'token_bucket', Limit: 50, Window: 10}")

	mockRepo := &MockRateLimiterRepository{}
	manager := NewRateLimiterManager(mockRepo)

	config := utils.RateLimiterConfig{
		Type:   constants.RateLimiterTypeTokenBucket,
		Limit:  50,
		Window: 10,
	}
	manager.AddLimiter("http://backend", config)

	ctx := context.Background()
	mockRepo.On("AllowTokenBucket", ctx, constants.RateLimitPrefix+"http://backend", 10, 50).Return(false, nil)

	t.Logf("Action: Allow(ctx, 'http://backend')")
	allowed, err := manager.Allow(ctx, "http://backend")
	t.Logf("Output: allowed=%t, err=%v", allowed, err)

	assert.NoError(t, err)
	t.Logf("Assertion: err == nil - PASSED")

	assert.False(t, allowed)
	t.Logf("Assertion: allowed == false - PASSED")

	mockRepo.AssertExpectations(t)
}

func TestRateLimiterManager_Allow_NoLimiter(t *testing.T) {
	t.Logf("Scenario: Rate Limiter Allow with No Limiter Configured")
	t.Logf("Input: backend='http://backend' with no limiter added")

	mockRepo := &MockRateLimiterRepository{}
	manager := NewRateLimiterManager(mockRepo)

	ctx := context.Background()
	t.Logf("Action: Allow(ctx, 'http://backend')")
	allowed, err := manager.Allow(ctx, "http://backend")
	t.Logf("Output: allowed=%t, err=%v", allowed, err)

	assert.NoError(t, err)
	t.Logf("Assertion: err == nil - PASSED")

	assert.True(t, allowed)
	t.Logf("Assertion: allowed == true (default allow) - PASSED")
}

func BenchmarkRateLimiterManager_Allow(b *testing.B) {
	b.Logf("Benchmark: Rate Limiter Manager Allow")
	b.Logf("Input: Parallel calls to Allow with sliding window config")

	mockRepo := &MockRateLimiterRepository{}
	manager := NewRateLimiterManager(mockRepo)

	config := utils.RateLimiterConfig{
		Type:   constants.RateLimiterTypeSlidingWindow,
		Limit:  1000,
		Window: 60,
	}
	manager.AddLimiter("http://backend", config)

	ctx := context.Background()
	mockRepo.On("Allow", ctx, constants.RateLimitPrefix+"http://backend", 1000, time.Minute).Return(true, nil).Maybe()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := manager.Allow(ctx, "http://backend")
			if err != nil {
				b.Errorf("Unexpected error: %v", err)
			}
		}
	})
	b.Logf("Output: Completed %d iterations", b.N)
}