package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"goproxy/internal/repository"
	"goproxy/internal/usecase"
	"goproxy/pkg/metrics"
	"goproxy/pkg/middleware"
	"goproxy/pkg/utils"
)

func main() {
	// Load config
	config, err := utils.LoadConfig("config.json") // or config.yaml
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize Prometheus metrics
	prometheus.MustRegister(metrics.TrafficSuccess, metrics.TrafficBlocked, metrics.CircuitState, metrics.RateLimitReached)

	// Initialize Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     config.Redis.Addr,
		Password: config.Redis.Password,
		DB:       config.Redis.DB,
	})

	// Initialize repositories
	rlRepo := repository.NewRedisRateLimiterRepository(rdb)

	// Initialize health checker
	healthChecker := usecase.NewHealthChecker(30 * time.Second) // Check every 30 seconds

	// Initialize usecases
	cbManager := usecase.NewCircuitBreakerManager()
	rlManager := usecase.NewRateLimiterManager(rlRepo, cbManager, healthChecker)
	proxy := usecase.NewHTTPProxy(cbManager, rlManager, config.EnableSingleflight)

	// Add circuit breakers and rate limiters for backends
	for _, backend := range config.Backends {
		cbManager.AddBreaker(backend.URL, backend.CircuitBreaker)
		rlManager.AddLimiter(backend.URL, backend.RateLimiter)
		for _, endpoint := range backend.Endpoints {
			rlManager.AddEndpointLimiter(backend.URL, endpoint.Path, endpoint.RateLimiter)
		}
	}

	// Start health checker worker
	hctx, hcancel := context.WithCancel(context.Background())
	defer hcancel()
	go healthChecker.Start(hctx, config.Backends)

	// HTTP handlers with panic recovery
	http.Handle("/metrics", middleware.PanicRecovery(promhttp.Handler()))

	http.Handle("/", middleware.PanicRecovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simple round-robin or select backend, for now use first
		if len(config.Backends) == 0 {
			http.Error(w, "No backends configured", http.StatusInternalServerError)
			return
		}
		backend := config.Backends[0] // TODO: implement load balancing
		endpoint := r.URL.Path // Use path as endpoint identifier
		if err := proxy.ForwardRequest(w, r, backend.URL, endpoint); err != nil {
			log.Printf("Proxy error: %v", err)
		}
	})))

	server := &http.Server{
		Addr:    config.ListenAddr,
		Handler: nil, // Uses default mux
	}

	// Channel to listen for interrupt signal
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	go func() {
		fmt.Printf("Starting proxy on %s\n", config.ListenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal
	<-done
	log.Println("Shutting down server...")

	// Create context with timeout for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Gracefully shutdown server
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}