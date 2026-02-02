package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/redis/go-redis/v9"
	"goproxy/internal/repository"
	"goproxy/internal/usecase"
	"goproxy/pkg/utils"
)

func main() {
	// Load config
	config, err := utils.LoadConfig("config.json") // or config.yaml
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     config.Redis.Addr,
		Password: config.Redis.Password,
		DB:       config.Redis.DB,
	})

	// Initialize repositories
	rlRepo := repository.NewRedisRateLimiterRepository(rdb)

	// Initialize usecases
	cbManager := usecase.NewCircuitBreakerManager()
	rlManager := usecase.NewRateLimiterManager(rlRepo)
	proxy := usecase.NewHTTPProxy(cbManager, rlManager, config.EnableSingleflight)

	// Add circuit breakers and rate limiters for backends
	for _, backend := range config.Backends {
		cbManager.AddBreaker(backend.URL, backend.CircuitBreaker)
		rlManager.AddLimiter(backend.URL, backend.RateLimiter)
	}

	// HTTP handler
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Simple round-robin or select backend, for now use first
		if len(config.Backends) == 0 {
			http.Error(w, "No backends configured", http.StatusInternalServerError)
			return
		}
		backend := config.Backends[0] // TODO: implement load balancing
		if err := proxy.ForwardRequest(w, r, backend.URL); err != nil {
			log.Printf("Proxy error: %v", err)
		}
	})

	fmt.Printf("Starting proxy on %s\n", config.ListenAddr)
	log.Fatal(http.ListenAndServe(config.ListenAddr, nil))
}