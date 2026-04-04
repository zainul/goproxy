package utils

import (
	"os"
	"testing"

	"goproxy/pkg/constants"

	"github.com/stretchr/testify/assert"
)

func TestLoadConfig_JSON(t *testing.T) {
	t.Logf("Scenario: %s", constants.TestScenarioConfigJSON)
	t.Logf("Input: JSON config data with listen_addr, enable_singleflight, redis, and backends including circuit_breaker and rate_limiter")

	configData := `{
		"listen_addr": ":8080",
		"enable_singleflight": true,
		"redis": {
			"addr": "localhost:6379",
			"password": "",
			"db": 0
		},
		"backends": [
			{
				"url": "http://backend1",
				"circuit_breaker": {
					"failure_threshold": 5,
					"success_threshold": 3,
					"timeout": "10s",
					"counter_type": "ringbuffer",
					"window_size": 10
				},
				"rate_limiter": {
					"type": "sliding_window",
					"limit": 100,
					"window": 60,
					"dynamic": true,
					"damage_level": 0.5,
					"catastrophic_level": 0.8,
					"healthy_increment": 0.01,
					"unhealthy_decrement": 0.01,
					"priority": 1,
					"health_adjustment_factor": 0.5,
					"readiness_adjustment_factor": 0.5,
					"success_rate_threshold": 0.8,
					"success_rate_adjustment_factor": 0.5
				},
				"endpoints": [
					{
						"path": "/api/test",
						"rate_limiter": {
							"type": "token_bucket",
							"limit": 50,
							"window": 10,
							"dynamic": false,
							"priority": 2
						}
					}
				]
			}
		]
	}`
	tmpFile, err := os.CreateTemp("", "config.json")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(configData)
	assert.NoError(t, err)
	tmpFile.Close()

	t.Logf("Action: LoadConfig with file %s", tmpFile.Name())
	config, err := LoadConfig(tmpFile.Name())
	t.Logf("Output: config=%+v, err=%v", config, err)

	assert.NoError(t, err)
	t.Logf("Assertion: No error occurred - PASSED")

	assert.Equal(t, ":8080", config.ListenAddr)
	t.Logf("Assertion: ListenAddr == ':8080' - PASSED")

	assert.True(t, config.EnableSingleflight)
	t.Logf("Assertion: EnableSingleflight == true - PASSED")

	assert.Equal(t, "localhost:6379", config.Redis.Addr)
	t.Logf("Assertion: Redis.Addr == 'localhost:6379' - PASSED")

	assert.Len(t, config.Backends, 1)
	t.Logf("Assertion: len(Backends) == 1 - PASSED")

	assert.Equal(t, "http://backend1", config.Backends[0].URL)
	t.Logf("Assertion: Backends[0].URL == 'http://backend1' - PASSED")

	assert.Equal(t, "ringbuffer", config.Backends[0].CircuitBreaker.CounterType)
	t.Logf("Assertion: Backends[0].CircuitBreaker.CounterType == 'ringbuffer' - PASSED")

	assert.Equal(t, "sliding_window", config.Backends[0].RateLimiter.Type)
	t.Logf("Assertion: Backends[0].RateLimiter.Type == 'sliding_window' - PASSED")

	assert.True(t, config.Backends[0].RateLimiter.Dynamic)
	t.Logf("Assertion: Backends[0].RateLimiter.Dynamic == true - PASSED")

	assert.Len(t, config.Backends[0].Endpoints, 1)
	t.Logf("Assertion: len(Backends[0].Endpoints) == 1 - PASSED")

	assert.Equal(t, "/api/test", config.Backends[0].Endpoints[0].Path)
	t.Logf("Assertion: Backends[0].Endpoints[0].Path == '/api/test' - PASSED")
}

func TestLoadConfig_YAML(t *testing.T) {
	t.Logf("Scenario: %s", constants.TestScenarioConfigYAML)
	t.Logf("Input: YAML config data with listen_addr, enable_singleflight, redis, and backends including circuit_breaker and rate_limiter")

	configData := `listen_addr: ":8080"
enable_singleflight: false
redis:
  addr: "localhost:6379"
  password: ""
  db: 0
backends:
  - url: "http://backend1"
    circuit_breaker:
      failure_threshold: 5
      success_threshold: 3
      timeout: "10s"
      counter_type: "sliding_window"
      window_size: 60
    rate_limiter:
      type: "token_bucket"
      limit: 50
      window: 10
      dynamic: true
      damage_level: 0.3
      catastrophic_level: 0.7
      healthy_increment: 0.02
      unhealthy_decrement: 0.02
      priority: 2
      health_adjustment_factor: 0.3
      readiness_adjustment_factor: 0.3
      success_rate_threshold: 0.9
      success_rate_adjustment_factor: 0.3
`
	tmpFile, err := os.CreateTemp("", "config")
	assert.NoError(t, err)
	yamlFile := tmpFile.Name() + ".yaml"
	os.Rename(tmpFile.Name(), yamlFile)
	defer os.Remove(yamlFile)

	tmpFile, err = os.Create(yamlFile)
	assert.NoError(t, err)
	_, err = tmpFile.WriteString(configData)
	assert.NoError(t, err)
	tmpFile.Close()

	t.Logf("Action: LoadConfig with file %s", yamlFile)
	config, err := LoadConfig(yamlFile)
	t.Logf("Output: config=%+v, err=%v", config, err)

	assert.NoError(t, err)
	t.Logf("Assertion: No error occurred - PASSED")

	assert.Equal(t, ":8080", config.ListenAddr)
	t.Logf("Assertion: ListenAddr == ':8080' - PASSED")

	assert.False(t, config.EnableSingleflight)
	t.Logf("Assertion: EnableSingleflight == false - PASSED")

	assert.Equal(t, "localhost:6379", config.Redis.Addr)
	t.Logf("Assertion: Redis.Addr == 'localhost:6379' - PASSED")

	assert.Len(t, config.Backends, 1)
	t.Logf("Assertion: len(Backends) == 1 - PASSED")

	assert.Equal(t, "http://backend1", config.Backends[0].URL)
	t.Logf("Assertion: Backends[0].URL == 'http://backend1' - PASSED")

	assert.Equal(t, "sliding_window", config.Backends[0].CircuitBreaker.CounterType)
	t.Logf("Assertion: Backends[0].CircuitBreaker.CounterType == 'sliding_window' - PASSED")

	assert.Equal(t, "token_bucket", config.Backends[0].RateLimiter.Type)
	t.Logf("Assertion: Backends[0].RateLimiter.Type == 'token_bucket' - PASSED")

	assert.True(t, config.Backends[0].RateLimiter.Dynamic)
	t.Logf("Assertion: Backends[0].RateLimiter.Dynamic == true - PASSED")

	assert.Equal(t, 0.02, config.Backends[0].RateLimiter.HealthyIncrement)
	t.Logf("Assertion: Backends[0].RateLimiter.HealthyIncrement == 0.02 - PASSED")
}

func TestValidateConfig_ValidConfig(t *testing.T) {
	config := &Config{
		ListenAddr: ":8080",
		Backends: []BackendConfig{
			{
				URL: "http://localhost:8080",
				CircuitBreaker: CircuitBreakerConfig{
					FailureThreshold: 5,
					SuccessThreshold: 3,
					Timeout:          "30s",
				},
				RateLimiter: RateLimiterConfig{
					Type:   "sliding_window",
					Limit:  100,
					Window: 60,
				},
			},
		},
		Redis: RedisConfig{
			Addr: "localhost:6379",
		},
		HealthCheckInterval: "30s",
	}

	err := ValidateConfig(config)
	assert.NoError(t, err)
	t.Logf("Assertion: Valid config passes validation - PASSED")
}

func TestValidateConfig_EmptyBackendURL(t *testing.T) {
	config := &Config{
		Backends: []BackendConfig{
			{
				URL: "",
				CircuitBreaker: CircuitBreakerConfig{
					FailureThreshold: 5,
					SuccessThreshold: 3,
					Timeout:          "30s",
				},
			},
		},
		Redis: RedisConfig{Addr: "localhost:6379"},
	}

	err := ValidateConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backends[0].url must not be empty")
	t.Logf("Assertion: Empty backend URL detected - PASSED")
}

func TestValidateConfig_InvalidBackendURL(t *testing.T) {
	config := &Config{
		Backends: []BackendConfig{
			{
				URL: "not-a-valid-url",
				CircuitBreaker: CircuitBreakerConfig{
					FailureThreshold: 5,
					SuccessThreshold: 3,
					Timeout:          "30s",
				},
			},
		},
		Redis: RedisConfig{Addr: "localhost:6379"},
	}

	err := ValidateConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backends[0].url is invalid")
	t.Logf("Assertion: Invalid backend URL detected - PASSED")
}

func TestValidateConfig_InvalidCircuitBreakerThresholds(t *testing.T) {
	config := &Config{
		Backends: []BackendConfig{
			{
				URL: "http://localhost:8080",
				CircuitBreaker: CircuitBreakerConfig{
					FailureThreshold: 0,
					SuccessThreshold: -1,
					Timeout:          "30s",
				},
			},
		},
		Redis: RedisConfig{Addr: "localhost:6379"},
	}

	err := ValidateConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit_breaker.failure_threshold must be positive")
	assert.Contains(t, err.Error(), "circuit_breaker.success_threshold must be positive")
	t.Logf("Assertion: Invalid circuit breaker thresholds detected - PASSED")
}

func TestValidateConfig_InvalidCircuitBreakerTimeout(t *testing.T) {
	config := &Config{
		Backends: []BackendConfig{
			{
				URL: "http://localhost:8080",
				CircuitBreaker: CircuitBreakerConfig{
					FailureThreshold: 5,
					SuccessThreshold: 3,
					Timeout:          "invalid-duration",
				},
			},
		},
		Redis: RedisConfig{Addr: "localhost:6379"},
	}

	err := ValidateConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit_breaker.timeout is invalid")
	t.Logf("Assertion: Invalid circuit breaker timeout detected - PASSED")
}

func TestValidateConfig_InvalidRateLimiterConfig(t *testing.T) {
	config := &Config{
		Backends: []BackendConfig{
			{
				URL: "http://localhost:8080",
				CircuitBreaker: CircuitBreakerConfig{
					FailureThreshold: 5,
					SuccessThreshold: 3,
					Timeout:          "30s",
				},
				RateLimiter: RateLimiterConfig{
					Type:   "sliding_window",
					Limit:  0,
					Window: -1,
				},
			},
		},
		Redis: RedisConfig{Addr: "localhost:6379"},
	}

	err := ValidateConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate_limiter.limit must be positive")
	assert.Contains(t, err.Error(), "rate_limiter.window must be positive")
	t.Logf("Assertion: Invalid rate limiter config detected - PASSED")
}

func TestValidateConfig_InvalidEndpointPath(t *testing.T) {
	config := &Config{
		Backends: []BackendConfig{
			{
				URL: "http://localhost:8080",
				CircuitBreaker: CircuitBreakerConfig{
					FailureThreshold: 5,
					SuccessThreshold: 3,
					Timeout:          "30s",
				},
				Endpoints: []EndpointConfig{
					{
						Path: "no-slash",
						RateLimiter: RateLimiterConfig{
							Limit:  10,
							Window: 60,
						},
					},
				},
			},
		},
		Redis: RedisConfig{Addr: "localhost:6379"},
	}

	err := ValidateConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "endpoints[0].path must start with /")
	t.Logf("Assertion: Invalid endpoint path detected - PASSED")
}

func TestValidateConfig_EmptyEndpointPath(t *testing.T) {
	config := &Config{
		Backends: []BackendConfig{
			{
				URL: "http://localhost:8080",
				CircuitBreaker: CircuitBreakerConfig{
					FailureThreshold: 5,
					SuccessThreshold: 3,
					Timeout:          "30s",
				},
				Endpoints: []EndpointConfig{
					{
						Path: "",
						RateLimiter: RateLimiterConfig{
							Limit:  10,
							Window: 60,
						},
					},
				},
			},
		},
		Redis: RedisConfig{Addr: "localhost:6379"},
	}

	err := ValidateConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "endpoints[0].path must not be empty")
	t.Logf("Assertion: Empty endpoint path detected - PASSED")
}

func TestValidateConfig_InvalidEndpointRateLimiter(t *testing.T) {
	config := &Config{
		Backends: []BackendConfig{
			{
				URL: "http://localhost:8080",
				CircuitBreaker: CircuitBreakerConfig{
					FailureThreshold: 5,
					SuccessThreshold: 3,
					Timeout:          "30s",
				},
				Endpoints: []EndpointConfig{
					{
						Path: "/api/test",
						RateLimiter: RateLimiterConfig{
							Limit:  0,
							Window: 0,
						},
					},
				},
			},
		},
		Redis: RedisConfig{Addr: "localhost:6379"},
	}

	err := ValidateConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "endpoints[0].rate_limiter.limit must be positive")
	assert.Contains(t, err.Error(), "endpoints[0].rate_limiter.window must be positive")
	t.Logf("Assertion: Invalid endpoint rate limiter detected - PASSED")
}

func TestValidateConfig_EmptyRedisAddr(t *testing.T) {
	config := &Config{
		Backends: []BackendConfig{
			{
				URL: "http://localhost:8080",
				CircuitBreaker: CircuitBreakerConfig{
					FailureThreshold: 5,
					SuccessThreshold: 3,
					Timeout:          "30s",
				},
			},
		},
		Redis: RedisConfig{Addr: ""},
	}

	err := ValidateConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis.addr must not be empty")
	t.Logf("Assertion: Empty Redis address detected - PASSED")
}

func TestValidateConfig_MultipleErrors(t *testing.T) {
	config := &Config{
		Backends: []BackendConfig{
			{
				URL: "",
				CircuitBreaker: CircuitBreakerConfig{
					FailureThreshold: 0,
					SuccessThreshold: 0,
					Timeout:          "invalid",
				},
			},
		},
		Redis: RedisConfig{Addr: ""},
	}

	err := ValidateConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backends[0].url must not be empty")
	assert.Contains(t, err.Error(), "circuit_breaker.failure_threshold must be positive")
	assert.Contains(t, err.Error(), "circuit_breaker.success_threshold must be positive")
	assert.Contains(t, err.Error(), "circuit_breaker.timeout is invalid")
	assert.Contains(t, err.Error(), "redis.addr must not be empty")
	t.Logf("Assertion: Multiple validation errors collected - PASSED")
}

func TestValidateConfig_ValidHealthCheckInterval(t *testing.T) {
	config := &Config{
		Backends: []BackendConfig{
			{
				URL: "http://localhost:8080",
				CircuitBreaker: CircuitBreakerConfig{
					FailureThreshold: 5,
					SuccessThreshold: 3,
					Timeout:          "30s",
				},
			},
		},
		Redis:               RedisConfig{Addr: "localhost:6379"},
		HealthCheckInterval: "30s",
	}

	err := ValidateConfig(config)
	assert.NoError(t, err)
	t.Logf("Assertion: Valid health check interval passes validation - PASSED")
}

func TestValidateConfig_EmptyHealthCheckInterval(t *testing.T) {
	config := &Config{
		Backends: []BackendConfig{
			{
				URL: "http://localhost:8080",
				CircuitBreaker: CircuitBreakerConfig{
					FailureThreshold: 5,
					SuccessThreshold: 3,
					Timeout:          "30s",
				},
			},
		},
		Redis:               RedisConfig{Addr: "localhost:6379"},
		HealthCheckInterval: "",
	}

	err := ValidateConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "health_check_interval must not be empty")
	t.Logf("Assertion: Empty health check interval detected - PASSED")
}

func TestValidateConfig_InvalidHealthCheckInterval(t *testing.T) {
	config := &Config{
		Backends: []BackendConfig{
			{
				URL: "http://localhost:8080",
				CircuitBreaker: CircuitBreakerConfig{
					FailureThreshold: 5,
					SuccessThreshold: 3,
					Timeout:          "30s",
				},
			},
		},
		Redis:               RedisConfig{Addr: "localhost:6379"},
		HealthCheckInterval: "invalid-duration",
	}

	err := ValidateConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "health_check_interval is invalid")
	t.Logf("Assertion: Invalid health check interval detected - PASSED")
}

func TestLoadConfig_DefaultHealthCheckInterval(t *testing.T) {
	configData := `{
		"listen_addr": ":8080",
		"enable_singleflight": true,
		"redis": {
			"addr": "localhost:6379",
			"password": "",
			"db": 0
		},
		"backends": [
			{
				"url": "http://backend1",
				"circuit_breaker": {
					"failure_threshold": 5,
					"success_threshold": 3,
					"timeout": "10s",
					"counter_type": "ringbuffer",
					"window_size": 10
				}
			}
		]
	}`
	tmpFile, err := os.CreateTemp("", "config.json")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(configData)
	assert.NoError(t, err)
	tmpFile.Close()

	config, err := LoadConfig(tmpFile.Name())
	assert.NoError(t, err)
	assert.Equal(t, "30s", config.HealthCheckInterval)
	t.Logf("Assertion: Default health check interval is '30s' - PASSED")
}

func TestLoadConfig_CustomHealthCheckInterval(t *testing.T) {
	configData := `{
		"listen_addr": ":8080",
		"enable_singleflight": true,
		"health_check_interval": "15s",
		"redis": {
			"addr": "localhost:6379",
			"password": "",
			"db": 0
		},
		"backends": [
			{
				"url": "http://backend1",
				"circuit_breaker": {
					"failure_threshold": 5,
					"success_threshold": 3,
					"timeout": "10s",
					"counter_type": "ringbuffer",
					"window_size": 10
				}
			}
		]
	}`
	tmpFile, err := os.CreateTemp("", "config.json")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(configData)
	assert.NoError(t, err)
	tmpFile.Close()

	config, err := LoadConfig(tmpFile.Name())
	assert.NoError(t, err)
	assert.Equal(t, "15s", config.HealthCheckInterval)
	t.Logf("Assertion: Custom health check interval is '15s' - PASSED")
}
