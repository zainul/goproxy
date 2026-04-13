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
	breakers sync.Map // map[string]*entity.CircuitBreaker - optimized for read-heavy workloads
	mu       sync.Mutex // protects state transitions only
}

// muLockBreakerAndFn is a helper to safely access breaker while updating state
func (m *CircuitBreakerManager) muLockBreakerAndFn(backendURL string, fn func(cb *entity.CircuitBreaker)) {
	if val, loaded := m.breakers.Load(backendURL); loaded {
		cb := val.(*entity.CircuitBreaker)
		fn(cb)
	}
}

// NewCircuitBreakerManager creates a new CircuitBreakerManager
func NewCircuitBreakerManager() *CircuitBreakerManager {
	return &CircuitBreakerManager{}
}

// AddBreaker adds a circuit breaker for a backend
func (m *CircuitBreakerManager) AddBreaker(backendURL string, config utils.CircuitBreakerConfig) {
	m.breakers.Store(backendURL, entity.NewCircuitBreaker(config))
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

	val, exists := m.breakers.Load(backendURL)
	if !exists {
		return true // Allow if no breaker
	}
	breaker := val.(*entity.CircuitBreaker)

	state := breaker.GetState()
	switch state {
	case entity.StateClosed:
		return true
	case entity.StateOpen:
		// Check if timeout has passed without updating breaker.LastFailTime (use stored value)
		if time.Since(breaker.LastFailTime) > breaker.Timeout {
			// Transition to half-open
			m.muLockBreakerAndFn(backendURL, func(cb *entity.CircuitBreaker) {
				cb.SetState(entity.StateHalfOpen)
				metrics.CircuitState.WithLabelValues(backendURL).Set(float64(entity.StateHalfOpen))
			})
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

	val, exists := m.breakers.Load(backendURL)
	if !exists {
		return
	}
	breaker := val.(*entity.CircuitBreaker)

	// Record in counter
	if breaker.CounterType == constants.CounterTypeRingBuffer {
		breaker.RingBuffer.Record(true)
	} else {
		breaker.SlidingWindow.Record(true)
	}

	state := breaker.GetState()
	if state == entity.StateHalfOpen {
		// Check if success threshold met
		if m.checkSuccessThreshold(breaker) {
			m.muLockBreakerAndFn(backendURL, func(cb *entity.CircuitBreaker) {
				cb.SetState(entity.StateClosed)
				metrics.CircuitState.WithLabelValues(backendURL).Set(float64(entity.StateClosed))
			})
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

	val, exists := m.breakers.Load(backendURL)
	if !exists {
		return
	}

	breaker := val.(*entity.CircuitBreaker)

	// Record in counter
	if breaker.CounterType == constants.CounterTypeRingBuffer {
		breaker.RingBuffer.Record(false)
	} else {
		breaker.SlidingWindow.Record(false)
	}

	m.muLockBreakerAndFn(backendURL, func(cb *entity.CircuitBreaker) {
		cb.SetLastFailTime(time.Now())
	})

	switch breaker.GetState() {
	case entity.StateClosed:
		if m.checkFailureThreshold(breaker) {
			m.muLockBreakerAndFn(backendURL, func(cb *entity.CircuitBreaker) {
				cb.SetState(entity.StateOpen)
				metrics.CircuitState.WithLabelValues(backendURL).Set(float64(entity.StateOpen))
			})
		}
	case entity.StateHalfOpen:
		m.muLockBreakerAndFn(backendURL, func(cb *entity.CircuitBreaker) {
			cb.SetState(entity.StateOpen)
			metrics.CircuitState.WithLabelValues(backendURL).Set(float64(entity.StateOpen))
		})
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

	val, exists := m.breakers.Load(backendURL)
	if !exists {
		return entity.StateClosed
	}
	breaker := val.(*entity.CircuitBreaker)
	return breaker.GetState()
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
