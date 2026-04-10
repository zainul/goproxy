package repository

import (
	"context"
	"testing"
	"time"
)

func TestNewMemoryRateLimiterRepository(t *testing.T) {
	repo := NewMemoryRateLimiterRepository()
	if repo == nil {
		t.Fatal("Expected non-nil repository")
	}
}

func TestMemoryRateLimiterAllow(t *testing.T) {
	repo := NewMemoryRateLimiterRepository()
	if repo == nil {
		t.Fatal("Expected non-nil repository")
	}

	ctx := context.Background()
	key := "test-key"
	limit := 10
	window := 1 * time.Second
	
	for i := 0; i < limit+2; i++ {
		allowed, err := repo.Allow(ctx, key, limit, window)
		if err != nil {
			t.Fatalf("Allow returned error: %v", err)
		}
		
		expectedAllowed := i < limit
		if allowed != expectedAllowed {
			t.Errorf("At iteration %d: expected allowed=%v, got allowed=%v", i, expectedAllowed, allowed)
		}
	}
}

func TestMemoryRateLimiterAllowReturnsTrueWhenUnderLimit(t *testing.T) {
	repo := NewMemoryRateLimiterRepository()
	
	ctx := context.Background()
	key := "test-key"
	limit := 5
	
	for i := 0; i < limit; i++ {
		allowed, err := repo.Allow(ctx, key, limit, time.Second)
		if err != nil {
			t.Fatalf("Allow returned error: %v", err)
		}
		if !allowed {
			t.Errorf("Expected allowed=true when under limit")
		}
	}
}

func TestMemoryRateLimiterAllowReturnsFalseWhenExceeded(t *testing.T) {
	repo := NewMemoryRateLimiterRepository()
	
	ctx := context.Background()
	key := "test-key"
	limit := 3
	
	for i := 0; i < limit+1; i++ {
		allowed, err := repo.Allow(ctx, key, limit, time.Second)
		if err != nil {
			t.Fatalf("Allow returned error: %v", err)
		}
		if i >= limit && allowed {
			t.Errorf("Expected allowed=false when rate limit exceeded")
		}
	}
}

func TestMemoryRateLimiterAllowReturnsBool(t *testing.T) {
	repo := NewMemoryRateLimiterRepository()
	
	ctx := context.Background()
	key := "test-key"
	limit := 100
	window := 1 * time.Second
	
	for i := 0; i < 5; i++ {
		allowed, err := repo.Allow(ctx, key, limit, window)
		if err != nil {
			t.Fatalf("Allow returned error: %v", err)
		}
		
		if !allowed && i < limit {
			t.Errorf("Expected allowed=true for valid requests")
		}
	}
}
