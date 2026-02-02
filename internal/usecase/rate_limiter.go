package usecase

import (
	"context"
	"fmt"
	"log"
	"math"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"goproxy/internal/entity"
	"goproxy/internal/repository"
	"goproxy/pkg/constants"
	"goproxy/pkg/utils"
)

// RateLimiterUsecase defines the interface for rate limiting
type RateLimiterUsecase interface {
	Allow(ctx context.Context, backendURL string) (bool, error)
}

// DynamicThreshold holds dynamic rate limit data
type DynamicThreshold struct {
	mu             sync.RWMutex
	CurrentLimit   int64
	BaseLimit      int
	LastAdjustment time.Time
	HealthyCount   int64
	UnhealthyCount int64
}

// RateLimiterManager implements RateLimiterUsecase
type RateLimiterManager struct {
	repo             repository.RateLimiterRepository
	limiters         map[string]*utils.RateLimiterConfig
	dynamicThresholds map[string]*DynamicThreshold
	cbManager        CircuitBreakerUsecase
}

// NewRateLimiterManager creates a new RateLimiterManager
func NewRateLimiterManager(repo repository.RateLimiterRepository, cbManager CircuitBreakerUsecase) *RateLimiterManager {
	return &RateLimiterManager{
		repo:              repo,
		limiters:          make(map[string]*utils.RateLimiterConfig),
		dynamicThresholds: make(map[string]*DynamicThreshold),
		cbManager:         cbManager,
	}
}

// AddLimiter adds a rate limiter for a backend
func (m *RateLimiterManager) AddLimiter(backendURL string, config utils.RateLimiterConfig) {
	m.limiters[backendURL] = &config
	if config.Dynamic {
		m.dynamicThresholds[backendURL] = &DynamicThreshold{
			CurrentLimit: int64(config.Limit),
			BaseLimit:    config.Limit,
		}
	}
}

// AddEndpointLimiter adds a rate limiter for a specific endpoint
func (m *RateLimiterManager) AddEndpointLimiter(backendURL, path string, config utils.RateLimiterConfig) {
	key := backendURL + path
	m.limiters[key] = &config
	if config.Dynamic {
		m.dynamicThresholds[key] = &DynamicThreshold{
			CurrentLimit: int64(config.Limit),
			BaseLimit:    config.Limit,
		}
	}
}

// Allow checks if the request is allowed
func (m *RateLimiterManager) Allow(ctx context.Context, backendURL string) (allowed bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic in RateLimiterManager.Allow: %v\nStack trace:\n%s", r, debug.Stack())
			err = fmt.Errorf("internal panic: %v", r)
		}
	}()
	config, exists := m.limiters[backendURL]
	if !exists {
		return true, nil // No limiter, allow
	}

	// Adjust dynamic threshold based on health
	limit := config.Limit
	if config.Dynamic {
		limit = m.adjustDynamicLimit(backendURL, config)
	}

	key := constants.RateLimitPrefix + backendURL

	switch config.Type {
	case constants.RateLimiterTypeSlidingWindow:
		return m.repo.Allow(ctx, key, limit, time.Duration(config.Window)*time.Second)
	case constants.RateLimiterTypeTokenBucket:
		return m.repo.AllowTokenBucket(ctx, key, config.Window, limit)
	default:
		return true, nil
	}
}

// AllowEndpoint checks if the request is allowed for a specific endpoint
func (m *RateLimiterManager) AllowEndpoint(ctx context.Context, backendURL, path string) (allowed bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic in RateLimiterManager.AllowEndpoint: %v\nStack trace:\n%s", r, debug.Stack())
			err = fmt.Errorf("internal panic: %v", r)
		}
	}()
	key := backendURL + path
	config, exists := m.limiters[key]
	if !exists {
		// Fall back to backend-wide limiter
		return m.Allow(ctx, backendURL)
	}

	// Adjust dynamic threshold based on health and priority
	limit := config.Limit
	if config.Dynamic {
		limit = m.adjustDynamicLimit(key, config)
	}

	rateKey := constants.RateLimitPrefix + key

	switch config.Type {
	case constants.RateLimiterTypeSlidingWindow:
		return m.repo.Allow(ctx, rateKey, limit, time.Duration(config.Window)*time.Second)
	case constants.RateLimiterTypeTokenBucket:
		return m.repo.AllowTokenBucket(ctx, rateKey, config.Window, limit)
	default:
		return true, nil
	}
}

// adjustDynamicLimit adjusts the limit based on upstream health and priority
func (m *RateLimiterManager) adjustDynamicLimit(backendURL string, config *utils.RateLimiterConfig) int {
	dynamic, exists := m.dynamicThresholds[backendURL]
	if !exists {
		return config.Limit
	}

	isHealthy := m.cbManager.GetState(backendURL) == entity.StateClosed

	now := time.Now()
	dynamic.mu.Lock()
	if now.Sub(dynamic.LastAdjustment) > time.Minute { // Adjust every minute
		if isHealthy {
			// Increase limit
			increment := int(float64(dynamic.BaseLimit) * config.HealthyIncrement)
			current := int(atomic.LoadInt64(&dynamic.CurrentLimit))
			newLimit := int(math.Min(float64(current+increment), float64(dynamic.BaseLimit*2)))
			atomic.StoreInt64(&dynamic.CurrentLimit, int64(newLimit))
			atomic.AddInt64(&dynamic.HealthyCount, 1)
		} else {
			// Decrease limit based on damage/catastrophic levels
			state := m.cbManager.GetState(backendURL)
			var decrement float64
			if state == entity.StateOpen {
				decrement = config.UnhealthyDecrement
			} else if state == entity.StateHalfOpen {
				decrement = config.UnhealthyDecrement * 0.5 // Less aggressive
			}
			decrementAmount := int(float64(dynamic.BaseLimit) * decrement)
			current := int(atomic.LoadInt64(&dynamic.CurrentLimit))
			newLimit := int(math.Max(float64(current-decrementAmount), float64(dynamic.BaseLimit/10)))
			atomic.StoreInt64(&dynamic.CurrentLimit, int64(newLimit))
			atomic.AddInt64(&dynamic.UnhealthyCount, 1)
		}
		dynamic.LastAdjustment = now
	}
	dynamic.mu.Unlock()

	// Apply priority multiplier (higher priority = higher limit)
	current := atomic.LoadInt64(&dynamic.CurrentLimit)
	priorityMultiplier := 1.0 + float64(config.Priority)*0.1 // 10% per priority level
	return int(float64(current) * priorityMultiplier)
}