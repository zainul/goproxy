package utils

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
	"goproxy/pkg/constants"
)

// RateLimiterConfig holds configuration for rate limiter
type RateLimiterConfig struct {
	Type                 string  `json:"type" yaml:"type"`                                 // "sliding_window" or "token_bucket"
	Limit                int     `json:"limit" yaml:"limit"`                               // requests per window for sliding, tokens for bucket
	Window               int     `json:"window" yaml:"window"`                             // window in seconds for sliding, refill rate per second for bucket
	Dynamic              bool    `json:"dynamic" yaml:"dynamic"`                           // enable dynamic rate limiting
	DamageLevel          float64 `json:"damage_level" yaml:"damage_level"`                 // damage level threshold (e.g., 0.5 for 50%)
	CatastrophicLevel    float64 `json:"catastrophic_level" yaml:"catastrophic_level"`     // catastrophic level threshold (e.g., 0.8)
	HealthyIncrement     float64 `json:"healthy_increment" yaml:"healthy_increment"`      // percentage increment when healthy (e.g., 0.01 for 1%)
	UnhealthyDecrement        float64 `json:"unhealthy_decrement" yaml:"unhealthy_decrement"`                      // percentage decrement when unhealthy (e.g., 0.01 for 1%)
	Priority                   int     `json:"priority" yaml:"priority"`                                                         // priority level (higher = more important)
	HealthAdjustmentFactor     float64 `json:"health_adjustment_factor" yaml:"health_adjustment_factor"`                       // factor to reduce limit when unhealthy (default 0.5)
	ReadinessAdjustmentFactor  float64 `json:"readiness_adjustment_factor" yaml:"readiness_adjustment_factor"`                 // factor to reduce limit when not ready (default 0.5)
	SuccessRateThreshold       float64 `json:"success_rate_threshold" yaml:"success_rate_threshold"`                           // threshold below which to adjust (default 0.8)
	SuccessRateAdjustmentFactor float64 `json:"success_rate_adjustment_factor" yaml:"success_rate_adjustment_factor"`         // factor based on success rate (default 0.5)
}

// CircuitBreakerConfig holds configuration for a circuit breaker
type CircuitBreakerConfig struct {
	FailureThreshold float64       `json:"failure_threshold" yaml:"failure_threshold"`
	SuccessThreshold float64       `json:"success_threshold" yaml:"success_threshold"`
	Timeout          string        `json:"timeout" yaml:"timeout"`
	CounterType      string        `json:"counter_type" yaml:"counter_type"` // "ringbuffer" or "sliding_window"
	WindowSize       int           `json:"window_size" yaml:"window_size"`   // for ringbuffer: count, for sliding_window: seconds
}

// EndpointConfig holds configuration for specific endpoints
type EndpointConfig struct {
	Path         string            `json:"path" yaml:"path"`                 // endpoint path (e.g., "/api/v1/*")
	RateLimiter  RateLimiterConfig `json:"rate_limiter" yaml:"rate_limiter"`
}

// BackendConfig holds configuration for a backend service
type BackendConfig struct {
	URL                 string               `json:"url" yaml:"url"`
	CircuitBreaker      CircuitBreakerConfig `json:"circuit_breaker" yaml:"circuit_breaker"`
	RateLimiter         RateLimiterConfig    `json:"rate_limiter" yaml:"rate_limiter"`
	Endpoints           []EndpointConfig     `json:"endpoints" yaml:"endpoints"`
	HealthCheckEndpoint string               `json:"health_check_endpoint" yaml:"health_check_endpoint"`
	ReadinessEndpoint   string               `json:"readiness_endpoint" yaml:"readiness_endpoint"`
	StatisticsEndpoint  string               `json:"statistics_endpoint" yaml:"statistics_endpoint"` // Optional
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Addr     string `json:"addr" yaml:"addr"`
	Password string `json:"password" yaml:"password"`
	DB       int    `json:"db" yaml:"db"`
}

// Config holds the overall configuration
type Config struct {
	ListenAddr         string           `json:"listen_addr" yaml:"listen_addr"`
	EnableSingleflight bool             `json:"enable_singleflight" yaml:"enable_singleflight"`
	Redis              RedisConfig      `json:"redis" yaml:"redis"`
	Backends           []BackendConfig `json:"backends" yaml:"backends"`
}

// LoadConfig loads configuration from JSON or YAML file
func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if filename[len(filename)-5:] == ".yaml" || filename[len(filename)-4:] == ".yml" {
		if err := yaml.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to decode YAML: %w", err)
		}
	} else {
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to decode JSON: %w", err)
		}
	}

	// Validate configurations
	for i, backend := range config.Backends {
		if backend.URL == "" {
			return nil, fmt.Errorf("backend %d: url is required", i)
		}
		if backend.CircuitBreaker.CounterType != constants.CounterTypeRingBuffer && backend.CircuitBreaker.CounterType != constants.CounterTypeSlidingWindow {
			return nil, fmt.Errorf("backend %d: invalid counter_type, must be '%s' or '%s'", i, constants.CounterTypeRingBuffer, constants.CounterTypeSlidingWindow)
		}
		if backend.RateLimiter.Type != "" && backend.RateLimiter.Type != constants.RateLimiterTypeSlidingWindow && backend.RateLimiter.Type != constants.RateLimiterTypeTokenBucket {
			return nil, fmt.Errorf("backend %d: invalid rate_limiter type, must be '%s' or '%s'", i, constants.RateLimiterTypeSlidingWindow, constants.RateLimiterTypeTokenBucket)
		}
	}

	return &config, nil
}