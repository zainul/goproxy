package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiterRepository defines the interface for rate limiting
type RateLimiterRepository interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
	AllowTokenBucket(ctx context.Context, key string, rate int, capacity int) (bool, error)
}

// RedisRateLimiterRepository implements RateLimiterRepository using Redis
type RedisRateLimiterRepository struct {
	client *redis.Client
}

// NewRedisRateLimiterRepository creates a new RedisRateLimiterRepository
func NewRedisRateLimiterRepository(client *redis.Client) *RedisRateLimiterRepository {
	return &RedisRateLimiterRepository{client: client}
}

// Allow implements sliding window rate limiting
func (r *RedisRateLimiterRepository) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	now := time.Now().Unix()
	windowStart := now - int64(window.Seconds())

	// Use a transaction to add current request and count
	pipe := r.client.TxPipeline()
	pipe.ZAdd(ctx, key, redis.Z{Member: now, Score: float64(now)})
	pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", windowStart))
	countCmd := pipe.ZCard(ctx, key)
	pipe.Expire(ctx, key, window)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, err
	}

	currentCount, err := countCmd.Result()
	if err != nil {
		return false, err
	}

	return currentCount <= int64(limit), nil
}

// AllowTokenBucket implements token bucket rate limiting
func (r *RedisRateLimiterRepository) AllowTokenBucket(ctx context.Context, key string, rate int, capacity int) (bool, error) {
	now := time.Now().Unix()

	// Lua script for token bucket
	script := `
	local key = KEYS[1]
	local rate = tonumber(ARGV[1])
	local capacity = tonumber(ARGV[2])
	local now = tonumber(ARGV[3])

	local data = redis.call('HMGET', key, 'tokens', 'last_refill')
	local tokens = tonumber(data[1]) or capacity
	local last_refill = tonumber(data[2]) or now

	local elapsed = now - last_refill
	local refill = math.floor(elapsed * rate)
	tokens = math.min(capacity, tokens + refill)

	local allowed = tokens > 0
	if allowed then
		tokens = tokens - 1
	end

	redis.call('HMSET', key, 'tokens', tokens, 'last_refill', now)
	redis.call('EXPIRE', key, math.ceil(capacity / rate) + 1)

	return allowed
	`

	result, err := r.client.Eval(ctx, script, []string{key}, rate, capacity, now).Result()
	if err != nil {
		return false, err
	}

	return result.(int64) == 1, nil
}