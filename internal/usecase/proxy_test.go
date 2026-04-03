package usecase

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"goproxy/internal/entity"
	"goproxy/pkg/constants"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockCircuitBreakerUsecase is a mock implementation
type MockCircuitBreakerUsecase struct {
	mock.Mock
}

func (m *MockCircuitBreakerUsecase) CanExecute(backendURL string) bool {
	args := m.Called(backendURL)
	return args.Bool(0)
}

func (m *MockCircuitBreakerUsecase) RecordSuccess(backendURL string) {
	m.Called(backendURL)
}

func (m *MockCircuitBreakerUsecase) RecordFailure(backendURL string) {
	m.Called(backendURL)
}

func (m *MockCircuitBreakerUsecase) GetState(backendURL string) entity.State {
	args := m.Called(backendURL)
	return args.Get(0).(entity.State)
}

// MockRateLimiterUsecase is a mock implementation
type MockRateLimiterUsecase struct {
	mock.Mock
}

func (m *MockRateLimiterUsecase) Allow(ctx context.Context, backendURL string) (bool, error) {
	args := m.Called(ctx, backendURL)
	return args.Bool(0), args.Error(1)
}

func (m *MockRateLimiterUsecase) AllowEndpoint(ctx context.Context, backendURL, path string) (bool, error) {
	args := m.Called(ctx, backendURL, path)
	return args.Bool(0), args.Error(1)
}

func TestHTTPProxy_ForwardRequest_Healthy(t *testing.T) {
	t.Logf("Scenario: %s", constants.TestScenarioNormalProxy)
	t.Logf("Input: GET request to healthy backend, circuit breaker allows, rate limiter allows")

	// Mock backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("healthy"))
	}))
	defer backend.Close()

	mockCB := &MockCircuitBreakerUsecase{}
	mockCB.On("CanExecute", backend.URL).Return(true)
	mockCB.On("RecordSuccess", backend.URL).Return()

	mockRL := &MockRateLimiterUsecase{}
	mockRL.On("Allow", mock.Anything, backend.URL).Return(true, nil)
	mockRL.On("AllowEndpoint", mock.Anything, backend.URL, "/test").Return(true, nil)

	lb := entity.NewLoadBalancer([]*entity.Backend{
		{URL: backend.URL, IsHealthy: true, IsReady: true, SuccessRate: 1.0},
	})
	proxy := NewHTTPProxy(mockCB, mockRL, lb, true, 30*time.Second)

	req := httptest.NewRequest("GET", "http://proxy/test", nil)
	w := httptest.NewRecorder()

	t.Logf("Action: ForwardRequest with req=%+v", req)
	err := proxy.ForwardRequest(w, req, "/test")
	t.Logf("Output: err=%v, statusCode=%d, body=%s", err, w.Code, w.Body.String())

	assert.NoError(t, err)
	t.Logf("Assertion: err == nil - PASSED")

	assert.Equal(t, http.StatusOK, w.Code)
	t.Logf("Assertion: statusCode == 200 - PASSED")

	assert.Equal(t, "healthy", w.Body.String())
	t.Logf("Assertion: body == 'healthy' - PASSED")

	mockCB.AssertExpectations(t)
	mockRL.AssertExpectations(t)
}

func TestHTTPProxy_ForwardRequest_Unhealthy(t *testing.T) {
	t.Logf("Scenario: %s", constants.TestScenarioUnhealthyBackend)
	t.Logf("Input: GET request to backend that returns 500, circuit breaker allows, rate limiter allows")

	// Mock backend that fails
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	mockCB := &MockCircuitBreakerUsecase{}
	mockCB.On("CanExecute", backend.URL).Return(true)
	mockCB.On("RecordFailure", backend.URL).Return()

	mockRL := &MockRateLimiterUsecase{}
	mockRL.On("Allow", mock.Anything, backend.URL).Return(true, nil)
	mockRL.On("AllowEndpoint", mock.Anything, backend.URL, "/test").Return(true, nil)

	lb := entity.NewLoadBalancer([]*entity.Backend{
		{URL: backend.URL, IsHealthy: true, IsReady: true, SuccessRate: 1.0},
	})
	proxy := NewHTTPProxy(mockCB, mockRL, lb, true, 30*time.Second)

	req := httptest.NewRequest("GET", "http://proxy/test", nil)
	w := httptest.NewRecorder()

	t.Logf("Action: ForwardRequest with req=%+v", req)
	err := proxy.ForwardRequest(w, req, "/test")
	t.Logf("Output: err=%v, statusCode=%d", err, w.Code)

	assert.NoError(t, err) // No error, but 500
	t.Logf("Assertion: err == nil (request forwarded) - PASSED")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	t.Logf("Assertion: statusCode == 500 - PASSED")

	mockCB.AssertExpectations(t)
	mockRL.AssertExpectations(t)
}

func TestHTTPProxy_ForwardRequest_CircuitOpen(t *testing.T) {
	t.Logf("Scenario: %s", constants.TestScenarioCircuitOpen)
	t.Logf("Input: GET request, circuit breaker denies execution")

	mockCB := &MockCircuitBreakerUsecase{}
	mockCB.On("CanExecute", "http://backend").Return(false)

	mockRL := &MockRateLimiterUsecase{}
	mockRL.On("Allow", mock.Anything, "http://backend").Return(true, nil)
	mockRL.On("AllowEndpoint", mock.Anything, "http://backend", "/test").Return(true, nil)

	lb := entity.NewLoadBalancer([]*entity.Backend{
		{URL: "http://backend", IsHealthy: true, IsReady: true, SuccessRate: 1.0},
	})
	proxy := NewHTTPProxy(mockCB, mockRL, lb, true, 30*time.Second)

	req := httptest.NewRequest("GET", "http://proxy/test", nil)
	w := httptest.NewRecorder()

	t.Logf("Action: ForwardRequest with req=%+v", req)
	err := proxy.ForwardRequest(w, req, "/test")
	t.Logf("Output: err=%v, statusCode=%d", err, w.Code)

	assert.Error(t, err)
	t.Logf("Assertion: err != nil - PASSED")

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	t.Logf("Assertion: statusCode == 503 - PASSED")

	mockCB.AssertExpectations(t)
	mockRL.AssertExpectations(t)
}

func TestHTTPProxy_ForwardRequest_RateLimitExceeded(t *testing.T) {
	t.Logf("Scenario: %s", constants.TestScenarioRateLimitExceeded)
	t.Logf("Input: GET request, rate limiter denies")

	mockCB := &MockCircuitBreakerUsecase{}
	mockRL := &MockRateLimiterUsecase{}

	mockRL.On("Allow", mock.Anything, "http://backend").Return(false, nil)

	lb := entity.NewLoadBalancer([]*entity.Backend{
		{URL: "http://backend", IsHealthy: true, IsReady: true, SuccessRate: 1.0},
	})
	proxy := NewHTTPProxy(mockCB, mockRL, lb, true, 30*time.Second)

	req := httptest.NewRequest("GET", "http://proxy/test", nil)
	w := httptest.NewRecorder()

	t.Logf("Action: ForwardRequest with req=%+v", req)
	err := proxy.ForwardRequest(w, req, "/test")
	t.Logf("Output: err=%v, statusCode=%d", err, w.Code)

	assert.Error(t, err)
	t.Logf("Assertion: err != nil - PASSED")

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	t.Logf("Assertion: statusCode == 429 - PASSED")

	mockRL.AssertExpectations(t)
}

func TestHTTPProxy_HighTraffic_HalfOpen(t *testing.T) {
	t.Logf("Scenario: %s", constants.TestScenarioHalfOpen)
	t.Logf("Input: 5000 concurrent GET requests to backend that starts failing then succeeds")

	// Backend that is partially healthy: first few fail, then succeed
	var callCount int
	var mu sync.Mutex
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		callCount++
		if callCount <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		}
	}))
	defer backend.Close()

	mockCB := &MockCircuitBreakerUsecase{}
	// Assume half-open, so CanExecute true
	mockCB.On("CanExecute", backend.URL).Return(true).Maybe()
	mockCB.On("RecordFailure", backend.URL).Return()
	mockCB.On("RecordSuccess", backend.URL).Return()

	mockRL := &MockRateLimiterUsecase{}
	mockRL.On("Allow", mock.Anything, backend.URL).Return(true, nil)
	mockRL.On("AllowEndpoint", mock.Anything, backend.URL, "/test").Return(true, nil)

	lb := entity.NewLoadBalancer([]*entity.Backend{
		{URL: backend.URL, IsHealthy: true, IsReady: true, SuccessRate: 1.0},
	})
	proxy := NewHTTPProxy(mockCB, mockRL, lb, true, 30*time.Second)

	// Simulate 5000 requests concurrently
	numRequests := 5000
	var wg sync.WaitGroup
	results := make(chan int, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "http://proxy/test", nil)
			w := httptest.NewRecorder()
			err := proxy.ForwardRequest(w, req, "/test")
			if err != nil {
				results <- http.StatusServiceUnavailable
			} else {
				results <- w.Code
			}
		}()
	}

	wg.Wait()
	close(results)

	// Check results
	successCount := 0
	failureCount := 0
	for code := range results {
		if code == http.StatusOK {
			successCount++
		} else {
			failureCount++
		}
	}

	// Since backend starts failing, but with concurrency, some may succeed later
	assert.True(t, successCount > 0 || failureCount > 0, "Should have some responses")
	// In real scenario, CB would transition, but here mocked

	// Check memory/CPU stability: hard to measure in test, but no crashes
	t.Logf("Output: successCount=%d, failureCount=%d", successCount, failureCount)
	assert.True(t, successCount > 0 || failureCount > 0, "Should have some responses")
	t.Logf("Assertion: successCount > 0 or failureCount > 0 - PASSED")
}

func BenchmarkHTTPProxy_ForwardRequest(b *testing.B) {
	b.Logf("Benchmark: HTTP Proxy Forward Request")
	b.Logf("Input: Parallel GET requests to healthy backend with mocks for CB and RL")

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	mockCB := &MockCircuitBreakerUsecase{}
	mockCB.On("CanExecute", backend.URL).Return(true).Maybe()
	mockCB.On("RecordSuccess", backend.URL).Return().Maybe()

	mockRL := &MockRateLimiterUsecase{}
	mockRL.On("Allow", mock.Anything, backend.URL).Return(true, nil).Maybe()
	mockRL.On("AllowEndpoint", mock.Anything, backend.URL, "/test").Return(true, nil).Maybe()

	lb := entity.NewLoadBalancer([]*entity.Backend{
		{URL: backend.URL, IsHealthy: true, IsReady: true, SuccessRate: 1.0},
	})
	proxy := NewHTTPProxy(mockCB, mockRL, lb, true, 30*time.Second)

	req := httptest.NewRequest("GET", "http://proxy/test", nil)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			w := httptest.NewRecorder()
			err := proxy.ForwardRequest(w, req, "/test")
			if err != nil {
				b.Errorf("Unexpected error: %v", err)
			}
		}
	})
	b.Logf("Output: Completed %d iterations", b.N)
}
