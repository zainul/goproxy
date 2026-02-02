package usecase

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"goproxy/internal/entity"
	"goproxy/pkg/utils"
)

func TestCircuitBreakerManager_CanExecute(t *testing.T) {
	manager := NewCircuitBreakerManager()

	// No breaker, should allow
	assert.True(t, manager.CanExecute("http://example.com"))

	// Add breaker
	config := utils.CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          "1s",
		CounterType:      "ringbuffer",
		WindowSize:       5,
	}
	manager.AddBreaker("http://example.com", config)

	// Initially closed
	assert.True(t, manager.CanExecute("http://example.com"))
	assert.Equal(t, entity.StateClosed, manager.GetState("http://example.com"))
}

func TestCircuitBreakerManager_RecordFailure(t *testing.T) {
	manager := NewCircuitBreakerManager()
	config := utils.CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          "1s",
		CounterType:      "ringbuffer",
		WindowSize:       5,
	}
	manager.AddBreaker("http://example.com", config)

	// Record failures
	manager.RecordFailure("http://example.com")
	assert.Equal(t, entity.StateClosed, manager.GetState("http://example.com"))

	manager.RecordFailure("http://example.com")
	assert.Equal(t, entity.StateOpen, manager.GetState("http://example.com"))
}

func TestCircuitBreakerManager_RecordSuccess(t *testing.T) {
	manager := NewCircuitBreakerManager()
	config := utils.CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          "1s",
		CounterType:      "ringbuffer",
		WindowSize:       5,
	}
	manager.AddBreaker("http://example.com", config)

	// Open it
	manager.RecordFailure("http://example.com")
	manager.RecordFailure("http://example.com")
	assert.Equal(t, entity.StateOpen, manager.GetState("http://example.com"))

	// Wait for timeout
	time.Sleep(time.Second + time.Millisecond)

	// Should allow in half-open
	assert.True(t, manager.CanExecute("http://example.com"))
	assert.Equal(t, entity.StateHalfOpen, manager.GetState("http://example.com"))

	// Record success
	manager.RecordSuccess("http://example.com")
	assert.Equal(t, entity.StateClosed, manager.GetState("http://example.com"))
}

func TestCircuitBreakerManager_SlidingWindow(t *testing.T) {
	manager := NewCircuitBreakerManager()
	config := utils.CircuitBreakerConfig{
		FailureThreshold: 0.6,
		SuccessThreshold: 0.7,
		Timeout:          "1s",
		CounterType:      "sliding_window",
		WindowSize:       1, // 1 second for test
	}
	manager.AddBreaker("http://example.com", config)

	// Record failures
	for i := 0; i < 6; i++ {
		manager.RecordFailure("http://example.com")
	}
	// Should open if rate > 0.6, but since all failures, rate=1
	assert.Equal(t, entity.StateOpen, manager.GetState("http://example.com"))
}