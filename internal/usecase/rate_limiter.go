package usecase

import (
	"context"
	"time"

	"goproxy/internal/repository"
	"goproxy/pkg/constants"
	"goproxy/pkg/utils"
)

// RateLimiterUsecase defines the interface for rate limiting
type RateLimiterUsecase interface {
	Allow(ctx context.Context, backendURL string) (bool, error)
}

// RateLimiterManager implements RateLimiterUsecase
type RateLimiterManager struct {
	repo     repository.RateLimiterRepository
	limiters map[string]*utils.RateLimiterConfig
}

// NewRateLimiterManager creates a new RateLimiterManager
func NewRateLimiterManager(repo repository.RateLimiterRepository) *RateLimiterManager {
	return &RateLimiterManager{
		repo:     repo,
		limiters: make(map[string]*utils.RateLimiterConfig),
	}
}

// AddLimiter adds a rate limiter for a backend
func (m *RateLimiterManager) AddLimiter(backendURL string, config utils.RateLimiterConfig) {
	m.limiters[backendURL] = &config
}

// Allow checks if the request is allowed
func (m *RateLimiterManager) Allow(ctx context.Context, backendURL string) (bool, error) {
	config, exists := m.limiters[backendURL]
	if !exists {
		return true, nil // No limiter, allow
	}

	key := constants.RateLimitPrefix + backendURL

	switch config.Type {
	case constants.RateLimiterTypeSlidingWindow:
		return m.repo.Allow(ctx, key, config.Limit, time.Duration(config.Window)*time.Second)
	case constants.RateLimiterTypeTokenBucket:
		return m.repo.AllowTokenBucket(ctx, key, config.Window, config.Limit) // rate = window, capacity = limit
	default:
		return true, nil
	}
}