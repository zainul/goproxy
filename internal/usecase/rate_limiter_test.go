package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"goproxy/internal/entity"
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



// MockHealthCheckerUsecase is a mock implementation
type MockHealthCheckerUsecase struct {
	mock.Mock
}

func (m *MockHealthCheckerUsecase) Start(ctx context.Context, backends []utils.BackendConfig) {
	m.Called(ctx, backends)
}

func (m *MockHealthCheckerUsecase) GetHealthStatus(backendURL string) *HealthStatus {
	args := m.Called(backendURL)
	if args.Get(0) == nil {
		return &HealthStatus{IsHealthy: true, IsReady: true, SuccessRate: 1.0}
	}
	return args.Get(0).(*HealthStatus)
}

func TestRateLimiterManager_Allow_SlidingWindow(t *testing.T) {
	t.Logf("Scenario: Rate Limiter Allow with Sliding Window")
	t.Logf("Input: backend='http://backend', config={Type: 'sliding_window', Limit: 100, Window: 60}")

	mockRepo := &MockRateLimiterRepository{}
	mockCB := &MockCircuitBreakerUsecase{}
	mockHC := &MockHealthCheckerUsecase{}
	mockHC.On("GetHealthStatus", mock.Anything).Return(&HealthStatus{IsHealthy: true, IsReady: true, SuccessRate: 1.0}).Maybe()
	manager := NewRateLimiterManager(mockRepo, mockCB, mockHC)

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
	mockCB := &MockCircuitBreakerUsecase{}
	mockHC := &MockHealthCheckerUsecase{}
	mockHC.On("GetHealthStatus", mock.Anything).Return(&HealthStatus{IsHealthy: true, IsReady: true, SuccessRate: 1.0}).Maybe()
	manager := NewRateLimiterManager(mockRepo, mockCB, mockHC)

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
	mockCB := &MockCircuitBreakerUsecase{}
	mockHC := &MockHealthCheckerUsecase{}
	mockHC.On("GetHealthStatus", mock.Anything).Return(&HealthStatus{IsHealthy: true, IsReady: true, SuccessRate: 1.0}).Maybe()
	manager := NewRateLimiterManager(mockRepo, mockCB, mockHC)

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
	mockCB := &MockCircuitBreakerUsecase{}
	mockHC := &MockHealthCheckerUsecase{}
	mockHC.On("GetHealthStatus", mock.Anything).Return(&HealthStatus{IsHealthy: true, IsReady: true, SuccessRate: 1.0}).Maybe()
	manager := NewRateLimiterManager(mockRepo, mockCB, mockHC)

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

func TestRateLimiterManager_Allow_Dynamic_Healthy(t *testing.T) {
	t.Logf("Scenario: Dynamic Rate Limiter with Healthy Upstream")
	t.Logf("Input: backend='http://backend' with dynamic config, upstream healthy")

	mockRepo := &MockRateLimiterRepository{}
	mockCB := &MockCircuitBreakerUsecase{}
	mockHC := &MockHealthCheckerUsecase{}
	mockHC.On("GetHealthStatus", mock.Anything).Return(&HealthStatus{IsHealthy: true, IsReady: true, SuccessRate: 1.0}).Maybe()
	manager := NewRateLimiterManager(mockRepo, mockCB, mockHC)

	config := utils.RateLimiterConfig{
		Type:               constants.RateLimiterTypeSlidingWindow,
		Limit:              100,
		Window:             60,
		Dynamic:            true,
		HealthyIncrement:   0.01,
		Priority:           1,
	}
	manager.AddLimiter("http://backend", config)

	mockCB.On("GetState", "http://backend").Return(entity.StateClosed) // Healthy
	mockHC.On("GetHealthStatus", "http://backend").Return(&HealthStatus{IsHealthy: true, IsReady: true, SuccessRate: 1.0})

	ctx := context.Background()
	// Expect adjusted limit: 100 + 1 (increment) = 101, then * 1.1 (priority) = 111
	mockRepo.On("Allow", ctx, constants.RateLimitPrefix+"http://backend", 111, time.Minute).Return(true, nil)

	t.Logf("Action: Allow(ctx, 'http://backend') with healthy upstream")
	allowed, err := manager.Allow(ctx, "http://backend")
	t.Logf("Output: allowed=%t, err=%v", allowed, err)

	assert.NoError(t, err)
	t.Logf("Assertion: err == nil - PASSED")

	assert.True(t, allowed)
	t.Logf("Assertion: allowed == true - PASSED")

	mockRepo.AssertExpectations(t)
	mockCB.AssertExpectations(t)
}

func TestRateLimiterManager_Allow_Dynamic_Unhealthy(t *testing.T) {
	t.Logf("Scenario: Dynamic Rate Limiter with Unhealthy Upstream")
	t.Logf("Input: backend='http://backend' with dynamic config, upstream open")

	mockRepo := &MockRateLimiterRepository{}
	mockCB := &MockCircuitBreakerUsecase{}
	mockHC := &MockHealthCheckerUsecase{}
	mockHC.On("GetHealthStatus", mock.Anything).Return(&HealthStatus{IsHealthy: true, IsReady: true, SuccessRate: 1.0}).Maybe()
	manager := NewRateLimiterManager(mockRepo, mockCB, mockHC)

	config := utils.RateLimiterConfig{
		Type:                 constants.RateLimiterTypeSlidingWindow,
		Limit:                100,
		Window:               60,
		Dynamic:              true,
		UnhealthyDecrement:   0.01,
		Priority:             1,
	}
	manager.AddLimiter("http://backend", config)

	mockCB.On("GetState", "http://backend").Return(entity.StateOpen) // Unhealthy
	mockHC.On("GetHealthStatus", "http://backend").Return(&HealthStatus{IsHealthy: false, IsReady: false, SuccessRate: 0.5})

	ctx := context.Background()
	// Expect adjusted limit: 100 * 0.99 (decrement) * 1.1 (priority) ≈ 108
	mockRepo.On("Allow", ctx, constants.RateLimitPrefix+"http://backend", 108, time.Minute).Return(false, nil)

	t.Logf("Action: Allow(ctx, 'http://backend') with unhealthy upstream")
	allowed, err := manager.Allow(ctx, "http://backend")
	t.Logf("Output: allowed=%t, err=%v", allowed, err)

	assert.NoError(t, err)
	t.Logf("Assertion: err == nil - PASSED")

	assert.False(t, allowed)
	t.Logf("Assertion: allowed == false - PASSED")

	mockRepo.AssertExpectations(t)
	mockCB.AssertExpectations(t)
}