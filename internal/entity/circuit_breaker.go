package entity

import (
	"sync"
	"sync/atomic"
	"time"

	"goproxy/pkg/constants"
	"goproxy/pkg/utils"
)

// State represents the circuit breaker state
type State int32

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

// String returns string representation of the state
func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF-OPEN"
	default:
		return "UNKNOWN"
	}
}

// CounterType represents the type of counter
type CounterType string

const (
	CounterRingBuffer   CounterType = "ringbuffer"
	CounterSlidingWindow CounterType = "sliding_window"
)

// Metric represents a single metric entry (success or failure)
type Metric struct {
	Timestamp time.Time
	Success   bool
}

// RingBufferCounter implements a fixed-size circular buffer counter
type RingBufferCounter struct {
	buffer []bool // true for success, false for failure
	size   int
	index  int32 // atomic for concurrency
	count  int32 // atomic count of entries
}

// NewRingBufferCounter creates a new RingBufferCounter
func NewRingBufferCounter(size int) *RingBufferCounter {
	return &RingBufferCounter{
		buffer: make([]bool, size),
		size:   size,
	}
}

// Record adds a new metric to the buffer
func (r *RingBufferCounter) Record(success bool) {
	idx := atomic.AddInt32(&r.index, 1) % int32(r.size)
	atomic.StoreInt32(&r.count, min(atomic.LoadInt32(&r.count)+1, int32(r.size)))
	r.buffer[idx] = success
}

// CountFailures returns the number of failures in the buffer
func (r *RingBufferCounter) CountFailures() int {
	failures := 0
	count := int(atomic.LoadInt32(&r.count))
	for i := 0; i < count; i++ {
		idx := (int(atomic.LoadInt32(&r.index)) - i + r.size) % r.size
		if !r.buffer[idx] {
			failures++
		}
	}
	return failures
}

// WindowSize returns the window size
func (r *RingBufferCounter) WindowSize() int {
	return r.size
}

// SlidingWindowCounter implements a time-based sliding window counter
type SlidingWindowCounter struct {
	window  time.Duration
	metrics []Metric
	mu      sync.RWMutex
}

// NewSlidingWindowCounter creates a new SlidingWindowCounter
func NewSlidingWindowCounter(window time.Duration) *SlidingWindowCounter {
	return &SlidingWindowCounter{
		window:  window,
		metrics: make([]Metric, 0),
	}
}

// Record adds a new metric
func (s *SlidingWindowCounter) Record(success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.metrics = append(s.metrics, Metric{Timestamp: now, Success: success})
	// Clean old metrics
	cutoff := now.Add(-s.window)
	for len(s.metrics) > 0 && s.metrics[0].Timestamp.Before(cutoff) {
		s.metrics = s.metrics[1:]
	}
}

// FailureRate returns the failure rate in the window
func (s *SlidingWindowCounter) FailureRate() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.metrics) == 0 {
		return 0.0
	}
	failures := 0
	for _, m := range s.metrics {
		if !m.Success {
			failures++
		}
	}
	return float64(failures) / float64(len(s.metrics))
}

// CircuitBreaker represents a circuit breaker for a backend
type CircuitBreaker struct {
	State            State
	FailureThreshold float64
	SuccessThreshold float64
	Timeout          time.Duration
	LastFailTime     time.Time
	CounterType      CounterType
	RingBuffer       *RingBufferCounter
	SlidingWindow    *SlidingWindowCounter
}

// NewCircuitBreaker creates a new CircuitBreaker
func NewCircuitBreaker(config utils.CircuitBreakerConfig) *CircuitBreaker {
	timeout, _ := time.ParseDuration(config.Timeout)
	cb := &CircuitBreaker{
		State:            StateClosed,
		FailureThreshold: config.FailureThreshold,
		SuccessThreshold: config.SuccessThreshold,
		Timeout:          timeout,
		CounterType:      CounterType(config.CounterType),
	}
	if config.CounterType == constants.CounterTypeSlidingWindow {
		cb.SlidingWindow = NewSlidingWindowCounter(time.Duration(config.WindowSize) * time.Second)
	} else {
		cb.RingBuffer = NewRingBufferCounter(config.WindowSize)
	}
	return cb
}



// min is a helper function
func min(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}