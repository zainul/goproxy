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
	CounterRingBuffer    CounterType = "ringbuffer"
	CounterSlidingWindow CounterType = "sliding_window"
)

// Metric represents a single metric entry (success or failure)
type Metric struct {
	Timestamp time.Time
	Success   bool
}

// RingBufferCounter implements a fixed-size circular buffer counter
type RingBufferCounter struct {
	buffer []int32 // 1 = success, 0 = failure (atomic-safe)
	size   int
	index  int32
	count  int32
}

// NewRingBufferCounter creates a new RingBufferCounter
func NewRingBufferCounter(size int) *RingBufferCounter {
	return &RingBufferCounter{
		buffer: make([]int32, size),
		size:   size,
	}
}

// Record adds a new metric to the buffer
func (r *RingBufferCounter) Record(success bool) {
	idx := atomic.AddInt32(&r.index, 1) % int32(r.size)
	currentCount := atomic.LoadInt32(&r.count)
	if currentCount < int32(r.size) {
		atomic.CompareAndSwapInt32(&r.count, currentCount, currentCount+1)
	}
	val := int32(0)
	if success {
		val = 1
	}
	atomic.StoreInt32(&r.buffer[idx], val)
}

// CountFailures returns the number of failures in the buffer
func (r *RingBufferCounter) CountFailures() int {
	failures := 0
	count := int(atomic.LoadInt32(&r.count))
	currentIdx := int(atomic.LoadInt32(&r.index))
	for i := 0; i < count; i++ {
		idx := (currentIdx - i + r.size) % r.size
		if atomic.LoadInt32(&r.buffer[idx]) == 0 {
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
	window     time.Duration
	metrics    []Metric
	mu         sync.Mutex // Use Mutex, not RWMutex — RWMutex has higher overhead for short critical sections
	totalCount int64      // atomic
	failCount  int64      // atomic — for fast FailureRate without lock
}

// NewSlidingWindowCounter creates a new SlidingWindowCounter
func NewSlidingWindowCounter(window time.Duration) *SlidingWindowCounter {
	return &SlidingWindowCounter{
		window:  window,
		metrics: make([]Metric, 0, 128), // Pre-allocate to reduce early reallocations
	}
}

// Record adds a new metric
func (s *SlidingWindowCounter) Record(success bool) {
	now := time.Now()

	s.mu.Lock()
	s.metrics = append(s.metrics, Metric{Timestamp: now, Success: success})

	// Cleanup expired entries
	cutoff := now.Add(-s.window)
	cutIdx := 0
	for cutIdx < len(s.metrics) && s.metrics[cutIdx].Timestamp.Before(cutoff) {
		cutIdx++
	}
	if cutIdx > 0 {
		// Remove expired entries and count removed failures
		removedFailures := int64(0)
		for i := 0; i < cutIdx; i++ {
			if !s.metrics[i].Success {
				removedFailures++
			}
		}
		s.metrics = s.metrics[cutIdx:]
		atomic.AddInt64(&s.failCount, -removedFailures)
	}
	s.mu.Unlock()

	// Update atomic counters outside the lock
	atomic.AddInt64(&s.totalCount, 1)
	if !success {
		atomic.AddInt64(&s.failCount, 1)
	}
}

// FailureRate returns the failure rate in the window
func (s *SlidingWindowCounter) FailureRate() float64 {
	total := atomic.LoadInt64(&s.totalCount)
	if total == 0 {
		return 0.0
	}
	failures := atomic.LoadInt64(&s.failCount)
	return float64(failures) / float64(total)
}

// CircuitBreaker represents a circuit breaker for a backend
type CircuitBreaker struct {
	state            int32 // atom
	FailureThreshold float64
	SuccessThreshold float64
	Timeout          time.Duration
	LastFailTime     time.Time
	lastFailMu       sync.Mutex // protects LastFailTime only
	CounterType      CounterType
	RingBuffer       *RingBufferCounter
	SlidingWindow    *SlidingWindowCounter
}

// GetState returns the current state atomically
func (cb *CircuitBreaker) GetState() State {
	return State(atomic.LoadInt32(&cb.state))
}

// SetState updates the state atomically
func (cb *CircuitBreaker) SetState(s State) {
	atomic.StoreInt32(&cb.state, int32(s))
}

// SetLastFailTime safely updates the last failure time
func (cb *CircuitBreaker) SetLastFailTime(t time.Time) {
	cb.lastFailMu.Lock()
	cb.LastFailTime = t
	cb.lastFailMu.Unlock()
}

// NewCircuitBreaker creates a new CircuitBreaker
func NewCircuitBreaker(config utils.CircuitBreakerConfig) *CircuitBreaker {
	timeout, _ := time.ParseDuration(config.Timeout)
	cb := &CircuitBreaker{
		state:            0,
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
