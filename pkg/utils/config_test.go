package utils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadConfig_JSON(t *testing.T) {
	configData := `{
		"listen_addr": ":8080",
		"enable_singleflight": true,
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
	assert.Equal(t, ":8080", config.ListenAddr)
	assert.True(t, config.EnableSingleflight)
	assert.Len(t, config.Backends, 1)
	assert.Equal(t, "http://backend1", config.Backends[0].URL)
	assert.Equal(t, "ringbuffer", config.Backends[0].CircuitBreaker.CounterType)
}

func TestLoadConfig_YAML(t *testing.T) {
	configData := `listen_addr: ":8080"
enable_singleflight: false
backends:
  - url: "http://backend1"
    circuit_breaker:
      failure_threshold: 5
      success_threshold: 3
      timeout: "10s"
      counter_type: "sliding_window"
      window_size: 60
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

	config, err := LoadConfig(yamlFile)
	assert.NoError(t, err)
	assert.Equal(t, ":8080", config.ListenAddr)
	assert.False(t, config.EnableSingleflight)
	assert.Len(t, config.Backends, 1)
	assert.Equal(t, "http://backend1", config.Backends[0].URL)
	assert.Equal(t, "sliding_window", config.Backends[0].CircuitBreaker.CounterType)
}