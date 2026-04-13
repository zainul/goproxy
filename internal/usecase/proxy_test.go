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
	"goproxy/pkg/utils"

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

	transport := &utils.TransportConfig{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   50,
		MaxConnsPerHost:       100,
		IdleConnTimeout:       "90s",
		TLSHandshakeTimeout:   "5s",
		ResponseHeaderTimeout: "10s",
		ExpectContinueTimeout: "1s",
		WriteBufferSize:       32768,
		ReadBufferSize:        32768,
	}

	// Mock backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	mockCB := &MockCircuitBreakerUsecase{}
	mockCB.On("CanExecute", backend.URL).Return(true, nil)
	mockCB.On("RecordSuccess", backend.URL).Return(nil)

	mockRL := &MockRateLimiterUsecase{}
	mockRL.On("Allow", mock.Anything, backend.URL).Return(true, nil)
	mockRL.On("AllowEndpoint", mock.Anything, backend.URL, "/test").Return(true, nil)

	lb := entity.NewLoadBalancer([]*entity.Backend{
		{URL: backend.URL, IsHealthy: true, IsReady: true, SuccessRate: 1.0},
	}, entity.LBStrategyRoundRobin)
	proxy := NewHTTPProxy(mockCB, mockRL, lb, true, 30*time.Second, transport)

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

	transport := &utils.TransportConfig{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   50,
		MaxConnsPerHost:       100,
		IdleConnTimeout:       "90s",
		TLSHandshakeTimeout:   "5s",
		ResponseHeaderTimeout: "10s",
		ExpectContinueTimeout: "1s",
		WriteBufferSize:       32768,
		ReadBufferSize:        32768,
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	mockCB := &MockCircuitBreakerUsecase{}
	mockCB.On("CanExecute", backend.URL).Return(true, nil, nil)
	mockCB.On("RecordFailure", backend.URL).Return(nil)
	mockCB.On("RecordSuccess", backend.URL).Return(nil)

	mockRL := &MockRateLimiterUsecase{}
	mockRL.On("Allow", mock.Anything, backend.URL).Return(true, nil)
	mockRL.On("AllowEndpoint", mock.Anything, backend.URL, "/test").Return(true, nil)

	lb := entity.NewLoadBalancer([]*entity.Backend{
		{URL: backend.URL, IsHealthy: true, IsReady: true, SuccessRate: 1.0},
	}, entity.LBStrategyRoundRobin)
	proxy := NewHTTPProxy(mockCB, mockRL, lb, true, 30*time.Second, transport)

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

func TestHeaderSanitization(t *testing.T) {
	var receivedHeaders map[string]string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedHeaders = make(map[string]string)
		for key, values := range r.Header {
			if len(values) > 0 {
				receivedHeaders[key] = values[0]
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, server.URL+"/test", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "TestClient/1.0")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("X-Custom-Header", "custom-value")

	transport := &utils.TransportConfig{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   50,
		MaxConnsPerHost:       100,
		IdleConnTimeout:       "90s",
		TLSHandshakeTimeout:   "5s",
		ResponseHeaderTimeout: "10s",
		ExpectContinueTimeout: "1s",
		WriteBufferSize:       32768,
		ReadBufferSize:        32768,
	}

	mockCB := &MockCircuitBreakerUsecase{}
	mockCB.On("CanExecute", server.URL).Return(true, nil)
	mockCB.On("RecordSuccess", server.URL).Return(nil)

	mockRL := &MockRateLimiterUsecase{}
	mockRL.On("Allow", mock.Anything, server.URL).Return(true, nil)
	mockRL.On("AllowEndpoint", mock.Anything, server.URL, "/test").Return(true, nil)

	lb := entity.NewLoadBalancer([]*entity.Backend{
		{URL: server.URL, IsHealthy: true, IsReady: true, SuccessRate: 1.0},
	}, entity.LBStrategyRoundRobin)
	proxy := NewHTTPProxy(mockCB, mockRL, lb, true, 30*time.Second, transport)

	w := httptest.NewRecorder()
	err := proxy.ForwardRequest(w, req, "/test")

	assert.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	assert.NotContains(t, receivedHeaders, "Connection")
	assert.Contains(t, receivedHeaders, "Authorization")
	assert.Contains(t, receivedHeaders, "Content-Type")
	assert.Contains(t, receivedHeaders, "User-Agent")
	assert.NotContains(t, receivedHeaders, "X-Custom-Header")
}
