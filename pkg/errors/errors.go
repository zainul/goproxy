package errors

import (
	"fmt"

	"goproxy/pkg/constants"
)

// AppError represents a structured error with user and developer messages
type AppError struct {
	UserMessage string
	DevMessage  string
	Err         error
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.DevMessage, e.Err)
	}
	return e.DevMessage
}

func (e *AppError) Unwrap() error {
	return e.Err
}

// UserError returns the user-friendly message
func (e *AppError) UserError() string {
	return e.UserMessage
}

// DevError returns the developer message
func (e *AppError) DevError() string {
	return e.DevMessage
}

// NewCircuitBreakerOpenError creates an error for circuit breaker open
func NewCircuitBreakerOpenError(backendURL string, err error) *AppError {
	return &AppError{
		UserMessage: constants.ErrCircuitBreakerOpenUser,
		DevMessage:  fmt.Sprintf("%s %s", constants.ErrCircuitBreakerOpenDev, backendURL),
		Err:         err,
	}
}

// NewRateLimitExceededError creates an error for rate limit exceeded
func NewRateLimitExceededError(backendURL string, err error) *AppError {
	return &AppError{
		UserMessage: constants.ErrRateLimitExceededUser,
		DevMessage:  fmt.Sprintf("%s %s", constants.ErrRateLimitExceededDev, backendURL),
		Err:         err,
	}
}

// NewInternalError creates an internal error
func NewInternalError(devMsg string, err error) *AppError {
	return &AppError{
		UserMessage: constants.ErrInternalErrorUser,
		DevMessage:  devMsg,
		Err:         err,
	}
}