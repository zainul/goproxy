package main

import (
	"fmt"
	"log"
	"net/http"

	"goproxy/internal/usecase"
	"goproxy/pkg/utils"
)

func main() {
	// Load config
	config, err := utils.LoadConfig("config.json") // or config.yaml
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize usecases
	cbManager := usecase.NewCircuitBreakerManager()
	proxy := usecase.NewHTTPProxy(cbManager, config.EnableSingleflight)

	// Add circuit breakers for backends
	for _, backend := range config.Backends {
		cbManager.AddBreaker(backend.URL, backend.CircuitBreaker)
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