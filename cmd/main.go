package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"goproxy/internal/entity"
	"goproxy/internal/repository"
	"goproxy/internal/usecase"
	"goproxy/pkg/metrics"
	"goproxy/pkg/middleware"
	"goproxy/pkg/utils"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
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
	healthCheckInterval, err := time.ParseDuration(config.HealthCheckInterval)
	if err != nil {
		log.Fatalf("Invalid health_check_interval: %v", err)
	}
	log.Printf("Starting health checker with interval: %s", healthCheckInterval)
	healthChecker := usecase.NewHealthChecker(healthCheckInterval)

	// Initialize usecases
	cbManager := usecase.NewCircuitBreakerManager()
	rlManager := usecase.NewRateLimiterManager(rlRepo, cbManager, healthChecker)

	// Initialize load balancer with all backends
	var backends []*entity.Backend
	for _, backendConfig := range config.Backends {
		backends = append(backends, &entity.Backend{
			URL:         backendConfig.URL,
			IsHealthy:   true,
			IsReady:     true,
			SuccessRate: 1.0,
		})
	}
	lb := entity.NewLoadBalancer(backends)

	proxy := usecase.NewHTTPProxy(cbManager, rlManager, lb, config.EnableSingleflight, 30*time.Second, &config.Transport)

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
	go healthChecker.Start(hctx, config.Backends)

	// HTTP handlers with panic recovery
	http.Handle("/metrics", middleware.PanicRecovery(promhttp.Handler()))

	http.Handle("/", middleware.PanicRecovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(config.Backends) == 0 {
			http.Error(w, "No backends configured", http.StatusInternalServerError)
			return
		}
		endpoint := r.URL.Path
		if err := proxy.ForwardRequest(w, r, endpoint); err != nil {
			log.Printf("Proxy error: %v", err)
		}
	})))

	server := &http.Server{
		Addr:    config.ListenAddr,
		Handler: http.DefaultServeMux,
	}

	// Channel to listen for interrupt signal
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	go func() {
		log.Printf("Starting proxy server on %s", config.ListenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal
	<-done
	log.Println("Shutting down server...")

	// Create context with timeout for graceful shutdown from config
	shutdownTimeout, err := time.ParseDuration(config.ShutdownTimeout)
	if err != nil {
		log.Printf("Invalid shutdown_timeout: %v, using default 30s", err)
		shutdownTimeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// Cancel health checker to stop background work and close Redis
	hcancel()

	// Gracefully shutdown server and close Redis connection
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	// Close Redis client connection
	if rdb != nil {
		if err := rdb.Close(); err != nil {
			log.Printf("Error closing Redis client: %v", err)
		}
	}

	log.Println("Server exited")
}
