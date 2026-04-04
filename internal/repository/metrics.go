// Package repository provides data access implementations.
//
// NOTE: InMemoryMetricsRepository is currently unused.
// It was designed for storing metrics in memory for:
// - Development/testing environments without Prometheus
// - Historical metrics aggregation
// - Dashboard data sources
//
// To use this repository:
// 1. Instantiate in main.go: metricsRepo := repository.NewInMemoryMetricsRepository()
// 2. Pass to usecases that need metrics storage
// 3. Call repository methods to record/retrieve metrics
//
// Consider deleting this file if not needed within 3 months.
package repository

import (
	"sync"
	"time"

	"goproxy/internal/entity"
)

// MetricsRepository defines the interface for storing and retrieving metrics
type MetricsRepository interface {
	RecordMetric(backendURL string, success bool)
	GetMetrics(backendURL string) []entity.Metric
}

// InMemoryMetricsRepository implements MetricsRepository using in-memory storage
type InMemoryMetricsRepository struct {
	data map[string][]entity.Metric
	mu   sync.RWMutex
}

// NewInMemoryMetricsRepository creates a new InMemoryMetricsRepository
func NewInMemoryMetricsRepository() *InMemoryMetricsRepository {
	return &InMemoryMetricsRepository{
		data: make(map[string][]entity.Metric),
	}
}

// RecordMetric records a metric for a backend
func (r *InMemoryMetricsRepository) RecordMetric(backendURL string, success bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[backendURL] = append(r.data[backendURL], entity.Metric{
		Timestamp: time.Now(),
		Success:   success,
	})
	// Optionally clean old metrics, but for simplicity, keep all
}

// GetMetrics retrieves metrics for a backend
func (r *InMemoryMetricsRepository) GetMetrics(backendURL string) []entity.Metric {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]entity.Metric(nil), r.data[backendURL]...) // copy
}