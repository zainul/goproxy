package usecase

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"goproxy/internal/entity"
	"goproxy/pkg/constants"
	"goproxy/pkg/utils"
)

func TestCircuitBreakerManager_CanExecute(t *testing.T) {
	t.Logf("Scenario: Circuit Breaker Can Execute")
	t.Logf("Input: backend='http://example.com' with ringbuffer config")

	manager := NewCircuitBreakerManager()

	// No breaker, should allow
	t.Logf("Action: CanExecute on non-existent backend")
	result1 := manager.CanExecute("http://example.com")
	t.Logf("Output: %t", result1)
	assert.True(t, result1)
	t.Logf("Assertion: CanExecute == true for no breaker - PASSED")

	// Add breaker
	config := utils.CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          "1s",
		CounterType:      constants.CounterTypeRingBuffer,
		WindowSize:       5,
	}
	manager.AddBreaker("http://example.com", config)

	// Initially closed
	t.Logf("Action: CanExecute on configured backend")
	result2 := manager.CanExecute("http://example.com")
	t.Logf("Output: %t", result2)
	assert.True(t, result2)
	t.Logf("Assertion: CanExecute == true initially - PASSED")

	state := manager.GetState("http://example.com")
	t.Logf("Output: State = %s", state)
	assert.Equal(t, entity.StateClosed, state)
	t.Logf("Assertion: State == CLOSED - PASSED")
}

func TestCircuitBreakerManager_RecordFailure(t *testing.T) {
	t.Logf("Scenario: Circuit Breaker Record Failure")
	t.Logf("Input: backend='http://example.com' with failure threshold 2")

	manager := NewCircuitBreakerManager()
	config := utils.CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          "1s",
		CounterType:      constants.CounterTypeRingBuffer,
		WindowSize:       5,
	}
	manager.AddBreaker("http://example.com", config)

	// Record failures
	t.Logf("Action: RecordFailure 1st time")
	manager.RecordFailure("http://example.com")
	state1 := manager.GetState("http://example.com")
	t.Logf("Output: State after 1 failure = %s", state1)
	assert.Equal(t, entity.StateClosed, state1)
	t.Logf("Assertion: State == CLOSED - PASSED")

	t.Logf("Action: RecordFailure 2nd time")
	manager.RecordFailure("http://example.com")
	state2 := manager.GetState("http://example.com")
	t.Logf("Output: State after 2 failures = %s", state2)
	assert.Equal(t, entity.StateOpen, state2)
	t.Logf("Assertion: State == OPEN - PASSED")
}

func TestCircuitBreakerManager_RecordSuccess(t *testing.T) {
	t.Logf("Scenario: Circuit Breaker Record Success in Half-Open")
	t.Logf("Input: backend='http://example.com' with success threshold 1")

	manager := NewCircuitBreakerManager()
	config := utils.CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          "1s",
		CounterType:      constants.CounterTypeRingBuffer,
		WindowSize:       5,
	}
	manager.AddBreaker("http://example.com", config)

	// Open it
	t.Logf("Action: Record 2 failures to open circuit")
	manager.RecordFailure("http://example.com")
	manager.RecordFailure("http://example.com")
	stateOpen := manager.GetState("http://example.com")
	t.Logf("Output: State after failures = %s", stateOpen)
	assert.Equal(t, entity.StateOpen, stateOpen)
	t.Logf("Assertion: State == OPEN - PASSED")

	// Wait for timeout
	t.Logf("Action: Wait for timeout to transition to half-open")
	time.Sleep(time.Second + time.Millisecond)

	// Should allow in half-open
	canExec := manager.CanExecute("http://example.com")
	t.Logf("Output: CanExecute = %t", canExec)
	assert.True(t, canExec)
	t.Logf("Assertion: CanExecute == true - PASSED")

	stateHalf := manager.GetState("http://example.com")
	t.Logf("Output: State = %s", stateHalf)
	assert.Equal(t, entity.StateHalfOpen, stateHalf)
	t.Logf("Assertion: State == HALF-OPEN - PASSED")

	// Record success
	t.Logf("Action: Record success")
	manager.RecordSuccess("http://example.com")
	stateClosed := manager.GetState("http://example.com")
	t.Logf("Output: State after success = %s", stateClosed)
	assert.Equal(t, entity.StateClosed, stateClosed)
	t.Logf("Assertion: State == CLOSED - PASSED")
}

func TestCircuitBreakerManager_SlidingWindow(t *testing.T) {
	t.Logf("Scenario: Circuit Breaker with Sliding Window Counter")
	t.Logf("Input: backend='http://example.com' with sliding window, failure threshold 0.6")

	manager := NewCircuitBreakerManager()
	config := utils.CircuitBreakerConfig{
		FailureThreshold: 0.6,
		SuccessThreshold: 0.7,
		Timeout:          "1s",
		CounterType:      constants.CounterTypeSlidingWindow,
		WindowSize:       1, // 1 second for test
	}
	manager.AddBreaker("http://example.com", config)

	// Record failures
	t.Logf("Action: Record 6 failures quickly")
	for i := 0; i < 6; i++ {
		manager.RecordFailure("http://example.com")
	}
	// Should open if rate > 0.6, but since all failures, rate=1
	state := manager.GetState("http://example.com")
	t.Logf("Output: State after failures = %s", state)
	assert.Equal(t, entity.StateOpen, state)
	t.Logf("Assertion: State == OPEN (due to high failure rate) - PASSED")
}