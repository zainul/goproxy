package utils

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"goproxy/pkg/constants"

	"gopkg.in/yaml.v2"
)

// RateLimiterConfig holds configuration for rate limiter
type RateLimiterConfig struct {
	Type                        string  `json:"type" yaml:"type"`                                                     // "sliding_window" or "token_bucket"
	Limit                       int     `json:"limit" yaml:"limit"`                                                   // requests per window for sliding, tokens for bucket
	Window                      int     `json:"window" yaml:"window"`                                                 // window in seconds for sliding, refill rate per second for bucket
	Dynamic                     bool    `json:"dynamic" yaml:"dynamic"`                                               // enable dynamic rate limiting
	DamageLevel                 float64 `json:"damage_level" yaml:"damage_level"`                                     // damage level threshold (e.g., 0.5 for 50%)
	CatastrophicLevel           float64 `json:"catastrophic_level" yaml:"catastrophic_level"`                         // catastrophic level threshold (e.g., 0.8)
	HealthyIncrement            float64 `json:"healthy_increment" yaml:"healthy_increment"`                           // percentage increment when healthy (e.g., 0.01 for 1%)
	UnhealthyDecrement          float64 `json:"unhealthy_decrement" yaml:"unhealthy_decrement"`                       // percentage decrement when unhealthy (e.g., 0.01 for 1%)
	Priority                    int     `json:"priority" yaml:"priority"`                                             // priority level (higher = more important)
	HealthAdjustmentFactor      float64 `json:"health_adjustment_factor" yaml:"health_adjustment_factor"`             // factor to reduce limit when unhealthy (default 0.5)
	ReadinessAdjustmentFactor   float64 `json:"readiness_adjustment_factor" yaml:"readiness_adjustment_factor"`       // factor to reduce limit when not ready (default 0.5)
	SuccessRateThreshold        float64 `json:"success_rate_threshold" yaml:"success_rate_threshold"`                 // threshold below which to adjust (default 0.8)
	SuccessRateAdjustmentFactor float64 `json:"success_rate_adjustment_factor" yaml:"success_rate_adjustment_factor"` // factor based on success rate (default 0.5)
}

// CircuitBreakerConfig holds configuration for a circuit breaker
type CircuitBreakerConfig struct {
	FailureThreshold float64 `json:"failure_threshold" yaml:"failure_threshold"`
	SuccessThreshold float64 `json:"success_threshold" yaml:"success_threshold"`
	Timeout          string  `json:"timeout" yaml:"timeout"`
	CounterType      string  `json:"counter_type" yaml:"counter_type"` // "ringbuffer" or "sliding_window"
	WindowSize       int     `json:"window_size" yaml:"window_size"`   // for ringbuffer: count, for sliding_window: seconds
}

// EndpointConfig holds configuration for specific endpoints
type EndpointConfig struct {
	Path        string            `json:"path" yaml:"path"` // endpoint path (e.g., "/api/v1/*")
	RateLimiter RateLimiterConfig `json:"rate_limiter" yaml:"rate_limiter"`
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

// TransportConfig holds HTTP transport tuning parameters
type TransportConfig struct {
	MaxIdleConns          int    `json:"max_idle_conns" yaml:"max_idle_conns"`
	MaxIdleConnsPerHost   int    `json:"max_idle_conns_per_host" yaml:"max_idle_conns_per_host"`
	MaxConnsPerHost       int    `json:"max_conns_per_host" yaml:"max_conns_per_host"`
	IdleConnTimeout       string `json:"idle_conn_timeout" yaml:"idle_conn_timeout"`
	TLSHandshakeTimeout   string `json:"tls_handshake_timeout" yaml:"tls_handshake_timeout"`
	ResponseHeaderTimeout string `json:"response_header_timeout" yaml:"response_header_timeout"`
	ExpectContinueTimeout string `json:"expect_continue_timeout" yaml:"expect_continue_timeout"`
	DisableKeepAlives     bool   `json:"disable_keep_alives" yaml:"disable_keep_alives"`
	DisableCompression    bool   `json:"disable_compression" yaml:"disable_compression"`
	WriteBufferSize       int    `json:"write_buffer_size" yaml:"write_buffer_size"`
	ReadBufferSize        int    `json:"read_buffer_size" yaml:"read_buffer_size"`
}

// ServerConfig holds HTTP server tuning parameters
type ServerConfig struct {
	ReadTimeout       string `json:"read_timeout" yaml:"read_timeout"`
	WriteTimeout      string `json:"write_timeout" yaml:"write_timeout"`
	ReadHeaderTimeout string `json:"read_header_timeout" yaml:"read_header_timeout"`
	IdleTimeout       string `json:"idle_timeout" yaml:"idle_timeout"`
	MaxHeaderBytes    int    `json:"max_header_bytes" yaml:"max_header_bytes"`
}

// Config holds the overall configuration
type Config struct {
	ListenAddr          string          `json:"listen_addr" yaml:"listen_addr"`
	EnableSingleflight  bool            `json:"enable_singleflight" yaml:"enable_singleflight"`
	ShutdownTimeout     string          `json:"shutdown_timeout" yaml:"shutdown_timeout"`
	RateLimiterStorage  string          `json:"rate_limiter_storage" yaml:"rate_limiter_storage"` // "memory" or "redis", default "memory"
	Redis               RedisConfig     `json:"redis" yaml:"redis"`
	Backends            []BackendConfig `json:"backends" yaml:"backends"`
	HealthCheckInterval string          `json:"health_check_interval" yaml:"health_check_interval"`
	Transport           TransportConfig `json:"transport" yaml:"transport"`
	Server              ServerConfig    `json:"server" yaml:"server"`
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

	// Set default health check interval if not specified
	if config.HealthCheckInterval == "" {
		config.HealthCheckInterval = "30s"
	}

	// Validate rate limiter storage type (default to "memory" if not specified)
	if config.RateLimiterStorage == "" {
		config.RateLimiterStorage = "memory"
		log.Println("Using in-memory rate limiter (default)")
	}

	validStorageTypes := map[string]bool{"memory": true, "redis": true}
	if !validStorageTypes[config.RateLimiterStorage] {
		return nil, fmt.Errorf("rate_limiter_storage must be 'memory' or 'redis', got '%s'", config.RateLimiterStorage)
	}

	// Set transport defaults for high throughput
	if config.Transport.MaxIdleConns == 0 {
		config.Transport.MaxIdleConns = 1000
	}
	if config.Transport.MaxIdleConnsPerHost == 0 {
		config.Transport.MaxIdleConnsPerHost = 200
	}
	if config.Transport.MaxConnsPerHost == 0 {
		config.Transport.MaxConnsPerHost = 500
	}
	if config.Transport.IdleConnTimeout == "" {
		config.Transport.IdleConnTimeout = "90s"
	}
	if config.Transport.TLSHandshakeTimeout == "" {
		config.Transport.TLSHandshakeTimeout = "5s"
	}
	if config.Transport.ResponseHeaderTimeout == "" {
		config.Transport.ResponseHeaderTimeout = "10s"
	}
	if config.Transport.ExpectContinueTimeout == "" {
		config.Transport.ExpectContinueTimeout = "1s"
	}
	if config.Transport.WriteBufferSize == 0 {
		config.Transport.WriteBufferSize = 32 * 1024 // 32KB
	}
	if config.Transport.ReadBufferSize == 0 {
		config.Transport.ReadBufferSize = 32 * 1024 // 32KB
	}

	// Set server defaults optimized for proxy workload
	if config.Server.ReadTimeout == "" {
		config.Server.ReadTimeout = "30s"
	}
	if config.Server.WriteTimeout == "" {
		config.Server.WriteTimeout = "60s" // Higher than read since we stream responses
	}
	if config.Server.ReadHeaderTimeout == "" {
		config.Server.ReadHeaderTimeout = "5s"
	}
	if config.Server.IdleTimeout == "" {
		config.Server.IdleTimeout = "120s"
	}
	if config.Server.MaxHeaderBytes == 0 {
		config.Server.MaxHeaderBytes = 64 * 1024 // 64KB, not 1MB
	}

	// Run comprehensive validation
	if err := ValidateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// ValidateConfig performs comprehensive validation of the loaded configuration
func ValidateConfig(config *Config) error {
	var errors []string

	for i, backend := range config.Backends {
		if backend.URL == "" {
			errors = append(errors, fmt.Sprintf("backends[%d].url must not be empty", i))
		} else {
			_, parseErr := url.ParseRequestURI(backend.URL)
			if parseErr != nil {
				errors = append(errors, fmt.Sprintf("backends[%d].url is invalid: %v", i, parseErr))
			}
		}

		if backend.CircuitBreaker.FailureThreshold <= 0 {
			errors = append(errors, fmt.Sprintf("backends[%d].circuit_breaker.failure_threshold must be positive", i))
		}

		if backend.CircuitBreaker.SuccessThreshold <= 0 {
			errors = append(errors, fmt.Sprintf("backends[%d].circuit_breaker.success_threshold must be positive", i))
		}

		_, durErr := time.ParseDuration(backend.CircuitBreaker.Timeout)
		if durErr != nil {
			errors = append(errors, fmt.Sprintf("backends[%d].circuit_breaker.timeout is invalid: %v", i, durErr))
		}

		if backend.RateLimiter.Type != "" {
			if backend.RateLimiter.Limit <= 0 {
				errors = append(errors, fmt.Sprintf("backends[%d].rate_limiter.limit must be positive", i))
			}

			if backend.RateLimiter.Window <= 0 {
				errors = append(errors, fmt.Sprintf("backends[%d].rate_limiter.window must be positive", i))
			}
		}

		for j, endpoint := range backend.Endpoints {
			if endpoint.Path == "" {
				errors = append(errors, fmt.Sprintf("backends[%d].endpoints[%d].path must not be empty", i, j))
			} else if !strings.HasPrefix(endpoint.Path, "/") {
				errors = append(errors, fmt.Sprintf("backends[%d].endpoints[%d].path must start with /", i, j))
			}

			if endpoint.RateLimiter.Limit <= 0 {
				errors = append(errors, fmt.Sprintf("backends[%d].endpoints[%d].rate_limiter.limit must be positive", i, j))
			}

			if endpoint.RateLimiter.Window <= 0 {
				errors = append(errors, fmt.Sprintf("backends[%d].endpoints[%d].rate_limiter.window must be positive", i, j))
			}
		}
	}

	if config.Redis.Addr == "" {
		errors = append(errors, "redis.addr must not be empty")
	}

	if config.HealthCheckInterval == "" {
		errors = append(errors, "health_check_interval must not be empty")
	} else {
		_, hcErr := time.ParseDuration(config.HealthCheckInterval)
		if hcErr != nil {
			errors = append(errors, fmt.Sprintf("health_check_interval is invalid: %v", hcErr))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("configuration validation failed:\n  - %s", strings.Join(errors, "\n  - "))
	}

	// Validate server config
	serverTimeouts := map[string]string{
		"server.read_timeout":        config.Server.ReadTimeout,
		"server.write_timeout":       config.Server.WriteTimeout,
		"server.read_header_timeout": config.Server.ReadHeaderTimeout,
		"server.idle_timeout":        config.Server.IdleTimeout,
	}
	for name, value := range serverTimeouts {
		if value != "" {
			if _, err := time.ParseDuration(value); err != nil {
				errors = append(errors, fmt.Sprintf("%s is invalid: %v", name, err))
			}
		}
	}
	if config.Server.MaxHeaderBytes < 0 {
		errors = append(errors, "server.max_header_bytes must be non-negative")
	}

	// Validate transport config
	if config.Transport.MaxIdleConns < 0 {
		errors = append(errors, "transport.max_idle_conns must be non-negative")
	}
	if config.Transport.MaxIdleConnsPerHost < 0 {
		errors = append(errors, "transport.max_idle_conns_per_host must be non-negative")
	}
	if config.Transport.MaxConnsPerHost < 0 {
		errors = append(errors, "transport.max_conns_per_host must be non-negative")
	}
	if config.Transport.IdleConnTimeout != "" {
		if _, err := time.ParseDuration(config.Transport.IdleConnTimeout); err != nil {
			errors = append(errors, fmt.Sprintf("transport.idle_conn_timeout is invalid: %v", err))
		}
	}
	if config.Transport.TLSHandshakeTimeout != "" {
		if _, err := time.ParseDuration(config.Transport.TLSHandshakeTimeout); err != nil {
			errors = append(errors, fmt.Sprintf("transport.tls_handshake_timeout is invalid: %v", err))
		}
	}
	if config.Transport.ResponseHeaderTimeout != "" {
		if _, err := time.ParseDuration(config.Transport.ResponseHeaderTimeout); err != nil {
			errors = append(errors, fmt.Sprintf("transport.response_header_timeout is invalid: %v", err))
		}
	}
	if config.Transport.ExpectContinueTimeout != "" {
		if _, err := time.ParseDuration(config.Transport.ExpectContinueTimeout); err != nil {
			errors = append(errors, fmt.Sprintf("transport.expect_continue_timeout is invalid: %v", err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("configuration validation failed:\n  - %s", strings.Join(errors, "\n  - "))
	}

	return nil
}
