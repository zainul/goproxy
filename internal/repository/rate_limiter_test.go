package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockRateLimiterRepository is a mock implementation
type MockRateLimiterRepository struct {
	mock.Mock
}

func (m *MockRateLimiterRepository) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	args := m.Called(ctx, key, limit, window)
	return args.Bool(0), args.Error(1)
}

func (m *MockRateLimiterRepository) AllowTokenBucket(ctx context.Context, key string, rate int, capacity int) (bool, error) {
	args := m.Called(ctx, key, rate, capacity)
	return args.Bool(0), args.Error(1)
}

func TestMockRateLimiterRepository(t *testing.T) {
	mockRepo := &MockRateLimiterRepository{}
	ctx := context.Background()

	mockRepo.On("Allow", ctx, "test", 10, time.Minute).Return(true, nil)
	mockRepo.On("AllowTokenBucket", ctx, "test", 5, 20).Return(false, nil)

	allowed, err := mockRepo.Allow(ctx, "test", 10, time.Minute)
	assert.NoError(t, err)
	assert.True(t, allowed)

	allowed, err = mockRepo.AllowTokenBucket(ctx, "test", 5, 20)
	assert.NoError(t, err)
	assert.False(t, allowed)

	mockRepo.AssertExpectations(t)
}