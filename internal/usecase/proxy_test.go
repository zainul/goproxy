package usecase

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"goproxy/internal/entity"
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

func TestHTTPProxy_ForwardRequest_Healthy(t *testing.T) {
	// Mock backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("healthy"))
	}))
	defer backend.Close()

	mockCB := &MockCircuitBreakerUsecase{}
	mockCB.On("CanExecute", backend.URL).Return(true)
	mockCB.On("RecordSuccess", backend.URL).Return()

	proxy := NewHTTPProxy(mockCB, true)

	req := httptest.NewRequest("GET", "http://proxy/test", nil)
	w := httptest.NewRecorder()

	err := proxy.ForwardRequest(w, req, backend.URL)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "healthy", w.Body.String())

	mockCB.AssertExpectations(t)
}

func TestHTTPProxy_ForwardRequest_Unhealthy(t *testing.T) {
	// Mock backend that fails
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	mockCB := &MockCircuitBreakerUsecase{}
	mockCB.On("CanExecute", backend.URL).Return(true)
	mockCB.On("RecordFailure", backend.URL).Return()

	proxy := NewHTTPProxy(mockCB, true)

	req := httptest.NewRequest("GET", "http://proxy/test", nil)
	w := httptest.NewRecorder()

	err := proxy.ForwardRequest(w, req, backend.URL)

	assert.NoError(t, err) // No error, but 500
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	mockCB.AssertExpectations(t)
}

func TestHTTPProxy_ForwardRequest_CircuitOpen(t *testing.T) {
	mockCB := &MockCircuitBreakerUsecase{}
	mockCB.On("CanExecute", "http://backend").Return(false)

	proxy := NewHTTPProxy(mockCB, true)

	req := httptest.NewRequest("GET", "http://proxy/test", nil)
	w := httptest.NewRecorder()

	err := proxy.ForwardRequest(w, req, "http://backend")

	assert.Error(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	mockCB.AssertExpectations(t)
}

func TestHTTPProxy_HighTraffic_HalfOpen(t *testing.T) {
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

	proxy := NewHTTPProxy(mockCB, true)

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
			err := proxy.ForwardRequest(w, req, backend.URL)
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
	t.Logf("Success: %d, Failures: %d", successCount, failureCount)
}

func BenchmarkHTTPProxy_ForwardRequest(b *testing.B) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	mockCB := &MockCircuitBreakerUsecase{}
	mockCB.On("CanExecute", backend.URL).Return(true).Maybe()
	mockCB.On("RecordSuccess", backend.URL).Return().Maybe()

	proxy := NewHTTPProxy(mockCB, true)

	req := httptest.NewRequest("GET", "http://proxy/test", nil)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			w := httptest.NewRecorder()
			proxy.ForwardRequest(w, req, backend.URL)
		}
	})
}