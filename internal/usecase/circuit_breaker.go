package usecase

import (
	"log"
	"runtime/debug"
	"sync"
	"time"

	"goproxy/internal/entity"
	"goproxy/pkg/constants"
	"goproxy/pkg/metrics"
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
	metrics.CircuitState.WithLabelValues(backendURL).Set(float64(entity.StateClosed))
}

// CanExecute checks if the request can be executed
func (m *CircuitBreakerManager) CanExecute(backendURL string) (canExecute bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic in CircuitBreakerManager.CanExecute: %v\nStack trace:\n%s", r, debug.Stack())
			canExecute = true // Allow on panic
		}
	}()
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
			metrics.CircuitState.WithLabelValues(backendURL).Set(float64(entity.StateHalfOpen))
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
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic in CircuitBreakerManager.RecordSuccess: %v\nStack trace:\n%s", r, debug.Stack())
		}
	}()
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
			metrics.CircuitState.WithLabelValues(backendURL).Set(float64(entity.StateClosed))
			m.mu.Unlock()
		}
	}
}

// RecordFailure records a failed call
func (m *CircuitBreakerManager) RecordFailure(backendURL string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic in CircuitBreakerManager.RecordFailure: %v\nStack trace:\n%s", r, debug.Stack())
		}
	}()
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
			metrics.CircuitState.WithLabelValues(backendURL).Set(float64(entity.StateOpen))
			m.mu.Unlock()
		}
	case entity.StateHalfOpen:
		m.mu.Lock()
		breaker.State = entity.StateOpen
		metrics.CircuitState.WithLabelValues(backendURL).Set(float64(entity.StateOpen))
		m.mu.Unlock()
	}
}

// GetState returns the current state
func (m *CircuitBreakerManager) GetState(backendURL string) (state entity.State) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic in CircuitBreakerManager.GetState: %v\nStack trace:\n%s", r, debug.Stack())
			state = entity.StateClosed // Default to closed
		}
	}()
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