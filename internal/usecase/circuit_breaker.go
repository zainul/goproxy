package usecase

import (
	"sync"
	"time"

	"goproxy/internal/entity"
	"goproxy/pkg/constants"
	"goproxy/pkg/utils"
)

// CircuitBreakerUsecase defines the interface for circuit breaker operations
type CircuitBreakerUsecase interface {
	CanExecute(backendURL string) bool
	RecordSuccess(backendURL string)
	RecordFailure(backendURL string)
	GetState(backendURL string) entity.State
}

// CircuitBreakerManager implements CircuitBreakerUsecase
type CircuitBreakerManager struct {
	breakers map[string]*entity.CircuitBreaker
	mu       sync.RWMutex
}

// NewCircuitBreakerManager creates a new CircuitBreakerManager
func NewCircuitBreakerManager() *CircuitBreakerManager {
	return &CircuitBreakerManager{
		breakers: make(map[string]*entity.CircuitBreaker),
	}
}

// AddBreaker adds a circuit breaker for a backend
func (m *CircuitBreakerManager) AddBreaker(backendURL string, config utils.CircuitBreakerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.breakers[backendURL] = entity.NewCircuitBreaker(config)
}

// CanExecute checks if the request can be executed
func (m *CircuitBreakerManager) CanExecute(backendURL string) bool {
	m.mu.RLock()
	breaker, exists := m.breakers[backendURL]
	m.mu.RUnlock()
	if !exists {
		return true // Allow if no breaker
	}

	switch breaker.State {
	case entity.StateClosed:
		return true
	case entity.StateOpen:
		if time.Since(breaker.LastFailTime) > breaker.Timeout {
			// Transition to half-open
			m.mu.Lock()
			breaker.State = entity.StateHalfOpen
			m.mu.Unlock()
			return true
		}
		return false
	case entity.StateHalfOpen:
		return true
	default:
		return false
	}
}

// RecordSuccess records a successful call
func (m *CircuitBreakerManager) RecordSuccess(backendURL string) {
	m.mu.RLock()
	breaker, exists := m.breakers[backendURL]
	m.mu.RUnlock()
	if !exists {
		return
	}

	// Record in counter
	if breaker.CounterType == constants.CounterTypeRingBuffer {
		breaker.RingBuffer.Record(true)
	} else {
		breaker.SlidingWindow.Record(true)
	}

	switch breaker.State {
	case entity.StateHalfOpen:
		// Check if success threshold met
		if m.checkSuccessThreshold(breaker) {
			m.mu.Lock()
			breaker.State = entity.StateClosed
			m.mu.Unlock()
		}
	}
}

// RecordFailure records a failed call
func (m *CircuitBreakerManager) RecordFailure(backendURL string) {
	m.mu.RLock()
	breaker, exists := m.breakers[backendURL]
	m.mu.RUnlock()
	if !exists {
		return
	}

	// Record in counter
	if breaker.CounterType == constants.CounterTypeRingBuffer {
		breaker.RingBuffer.Record(false)
	} else {
		breaker.SlidingWindow.Record(false)
	}
	breaker.LastFailTime = time.Now()

	switch breaker.State {
	case entity.StateClosed:
		if m.checkFailureThreshold(breaker) {
			m.mu.Lock()
			breaker.State = entity.StateOpen
			m.mu.Unlock()
		}
	case entity.StateHalfOpen:
		m.mu.Lock()
		breaker.State = entity.StateOpen
		m.mu.Unlock()
	}
}

// GetState returns the current state
func (m *CircuitBreakerManager) GetState(backendURL string) entity.State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if breaker, exists := m.breakers[backendURL]; exists {
		return breaker.State
	}
	return entity.StateClosed
}

// checkFailureThreshold checks if failure threshold is exceeded
func (m *CircuitBreakerManager) checkFailureThreshold(breaker *entity.CircuitBreaker) bool {
	if breaker.CounterType == constants.CounterTypeRingBuffer {
		failures := breaker.RingBuffer.CountFailures()
		return float64(failures) >= breaker.FailureThreshold
	} else {
		rate := breaker.SlidingWindow.FailureRate()
		return rate >= breaker.FailureThreshold
	}
}

// checkSuccessThreshold checks if success threshold is met in half-open
func (m *CircuitBreakerManager) checkSuccessThreshold(breaker *entity.CircuitBreaker) bool {
	if breaker.CounterType == constants.CounterTypeRingBuffer {
		// For ringbuffer, count successes
		successes := breaker.RingBuffer.WindowSize() - breaker.RingBuffer.CountFailures() // assuming full window
		return float64(successes) >= breaker.SuccessThreshold
	} else {
		rate := 1.0 - breaker.SlidingWindow.FailureRate()
		return rate >= breaker.SuccessThreshold
	}
}