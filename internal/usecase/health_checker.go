package usecase

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"goproxy/pkg/utils"
)

// HealthStatus represents the health check result
type HealthStatus struct {
	IsHealthy   bool
	IsReady     bool
	SuccessRate float64 // From statistics endpoint, if available
	LastChecked time.Time
}

// StatisticsResponse represents the expected response from statistics endpoint
type StatisticsResponse struct {
	SuccessRate float64 `json:"success_rate"`
}

// HealthCheckerUsecase manages health checks for backends
type HealthCheckerUsecase interface {
	Start(ctx context.Context, backends []utils.BackendConfig)
	GetHealthStatus(backendURL string) *HealthStatus
}

// HealthChecker implements HealthCheckerUsecase
type HealthChecker struct {
	statuses map[string]*HealthStatus
	mu       sync.RWMutex
	client   *http.Client
	interval time.Duration
}

// NewHealthChecker creates a new HealthChecker
func NewHealthChecker(interval time.Duration) *HealthChecker {
	return &HealthChecker{
		statuses: make(map[string]*HealthStatus),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		interval: interval,
	}
}

// Start begins the health checking loop
func (h *HealthChecker) Start(ctx context.Context, backends []utils.BackendConfig) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	// Initial check
	h.checkAllBackends(backends)

	for {
		select {
		case <-ctx.Done():
			log.Println("Health checker stopped")
			return
		case <-ticker.C:
			h.checkAllBackends(backends)
		}
	}
}

// checkAllBackends performs health checks for all backends
func (h *HealthChecker) checkAllBackends(backends []utils.BackendConfig) {
	for _, backend := range backends {
		status := h.checkBackend(backend)
		h.mu.Lock()
		h.statuses[backend.URL] = status
		h.mu.Unlock()
	}
}

// checkBackend performs health checks for a single backend
func (h *HealthChecker) checkBackend(backend utils.BackendConfig) *HealthStatus {
	status := &HealthStatus{
		LastChecked: time.Now(),
	}

	// Check health
	if backend.HealthCheckEndpoint != "" {
		status.IsHealthy = h.checkEndpoint(backend.URL + backend.HealthCheckEndpoint)
	} else {
		// If no health endpoint, assume healthy
		status.IsHealthy = true
	}

	// Check readiness
	if backend.ReadinessEndpoint != "" {
		status.IsReady = h.checkEndpoint(backend.URL + backend.ReadinessEndpoint)
	} else {
		// If no readiness endpoint, assume ready
		status.IsReady = true
	}

	// Get statistics
	if backend.StatisticsEndpoint != "" {
		status.SuccessRate = h.getSuccessRate(backend.URL + backend.StatisticsEndpoint)
	} else {
		// Default success rate
		status.SuccessRate = 1.0
	}

	return status
}

// checkEndpoint checks if an endpoint returns 200
func (h *HealthChecker) checkEndpoint(url string) bool {
	resp, err := h.client.Get(url)
	if err != nil {
		log.Printf("Health check failed for %s: %v", url, err)
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// getSuccessRate fetches success rate from statistics endpoint
func (h *HealthChecker) getSuccessRate(url string) float64 {
	resp, err := h.client.Get(url)
	if err != nil {
		log.Printf("Statistics fetch failed for %s: %v", url, err)
		return 1.0 // Default to 100%
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Statistics endpoint returned status %d for %s", resp.StatusCode, url)
		return 1.0
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read statistics response: %v", err)
		return 1.0
	}

	var stats StatisticsResponse
	if err := json.Unmarshal(body, &stats); err != nil {
		log.Printf("Failed to parse statistics response: %v", err)
		return 1.0
	}

	return stats.SuccessRate
}

// GetHealthStatus returns the health status for a backend
func (h *HealthChecker) GetHealthStatus(backendURL string) *HealthStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if status, exists := h.statuses[backendURL]; exists {
		return status
	}
	// Return default healthy status
	return &HealthStatus{
		IsHealthy:   true,
		IsReady:     true,
		SuccessRate: 1.0,
		LastChecked: time.Now(),
	}
}