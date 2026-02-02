package utils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"goproxy/pkg/constants"
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
					"window": 60
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
}