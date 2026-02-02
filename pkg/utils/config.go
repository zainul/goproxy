package utils

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

// CircuitBreakerConfig holds configuration for a circuit breaker
type CircuitBreakerConfig struct {
	FailureThreshold float64       `json:"failure_threshold" yaml:"failure_threshold"`
	SuccessThreshold float64       `json:"success_threshold" yaml:"success_threshold"`
	Timeout          string        `json:"timeout" yaml:"timeout"`
	CounterType      string        `json:"counter_type" yaml:"counter_type"` // "ringbuffer" or "sliding_window"
	WindowSize       int           `json:"window_size" yaml:"window_size"`   // for ringbuffer: count, for sliding_window: seconds
}

// BackendConfig holds configuration for a backend service
type BackendConfig struct {
	URL             string               `json:"url" yaml:"url"`
	CircuitBreaker  CircuitBreakerConfig `json:"circuit_breaker" yaml:"circuit_breaker"`
}

// Config holds the overall configuration
type Config struct {
	ListenAddr         string           `json:"listen_addr" yaml:"listen_addr"`
	EnableSingleflight bool             `json:"enable_singleflight" yaml:"enable_singleflight"`
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
		if backend.CircuitBreaker.CounterType != "ringbuffer" && backend.CircuitBreaker.CounterType != "sliding_window" {
			return nil, fmt.Errorf("backend %d: invalid counter_type, must be 'ringbuffer' or 'sliding_window'", i)
		}
	}

	return &config, nil
}