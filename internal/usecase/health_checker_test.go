package usecase

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"goproxy/pkg/utils"
)

func TestHealthChecker_StartAndGetHealthStatus(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		case "/ready":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ready":true}`))
		case "/stats":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success_rate": 0.95,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer backend.Close()

	backends := []utils.BackendConfig{
		{
			URL:                 backend.URL,
			HealthCheckEndpoint: "/health",
			ReadinessEndpoint:   "/ready",
			StatisticsEndpoint:  "/stats",
		},
	}

	hc := NewHealthChecker(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hc.Start(ctx, backends)

	time.Sleep(200 * time.Millisecond)

	status := hc.GetHealthStatus(backend.URL)
	if status == nil {
		t.Fatal("expected health status to exist")
	}

	if !status.IsHealthy {
		t.Errorf("expected backend to be healthy, got false")
	}

	if !status.IsReady {
		t.Errorf("expected backend to be ready, got false")
	}

	if status.SuccessRate != 0.95 {
		t.Errorf("expected success rate 0.95, got %f", status.SuccessRate)
	}

	if status.LastChecked.IsZero() {
		t.Error("expected LastChecked to be set")
	}
}

func TestHealthChecker_UnhealthyBackend(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"unhealthy"}`))
		case "/ready":
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"ready":false}`))
		case "/stats":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success_rate": 0.2,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer backend.Close()

	backends := []utils.BackendConfig{
		{
			URL:                 backend.URL,
			HealthCheckEndpoint: "/health",
			ReadinessEndpoint:   "/ready",
			StatisticsEndpoint:  "/stats",
		},
	}

	hc := NewHealthChecker(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hc.Start(ctx, backends)

	time.Sleep(200 * time.Millisecond)

	status := hc.GetHealthStatus(backend.URL)
	if status == nil {
		t.Fatal("expected health status to exist")
	}

	if status.IsHealthy {
		t.Errorf("expected backend to be unhealthy, got true")
	}

	if status.IsReady {
		t.Errorf("expected backend to not be ready, got true")
	}

	if status.SuccessRate != 0.2 {
		t.Errorf("expected success rate 0.2, got %f", status.SuccessRate)
	}
}

func TestHealthChecker_MissingEndpoints(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer backend.Close()

	backends := []utils.BackendConfig{
		{
			URL:                 backend.URL,
			HealthCheckEndpoint: "/health",
			ReadinessEndpoint:   "/ready",
			StatisticsEndpoint:  "/stats",
		},
	}

	hc := NewHealthChecker(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hc.Start(ctx, backends)

	time.Sleep(200 * time.Millisecond)

	status := hc.GetHealthStatus(backend.URL)
	if status == nil {
		t.Fatal("expected health status to exist")
	}

	if status.IsHealthy {
		t.Errorf("expected backend to be unhealthy when endpoints missing, got true")
	}

	if status.IsReady {
		t.Errorf("expected backend to not be ready when endpoints missing, got true")
	}
}

func TestHealthChecker_ContextCancellation(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backends := []utils.BackendConfig{
		{
			URL:                 backend.URL,
			HealthCheckEndpoint: "/health",
		},
	}

	hc := NewHealthChecker(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		hc.Start(ctx, backends)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)

	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("health checker did not stop after context cancellation")
	}
}

func TestHealthChecker_GetHealthStatusNonExistent(t *testing.T) {
	hc := NewHealthChecker(1 * time.Second)

	status := hc.GetHealthStatus("http://nonexistent:8080")

	if status == nil {
		t.Fatal("expected default health status for non-existent backend")
	}

	if !status.IsHealthy {
		t.Errorf("expected default status to be healthy, got false")
	}

	if !status.IsReady {
		t.Errorf("expected default status to be ready, got false")
	}

	if status.SuccessRate != 1.0 {
		t.Errorf("expected default success rate 1.0, got %f", status.SuccessRate)
	}
}

func TestHealthChecker_MultipleBackends(t *testing.T) {
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer backend1.Close()

	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer backend2.Close()

	backends := []utils.BackendConfig{
		{
			URL:                 backend1.URL,
			HealthCheckEndpoint: "/health",
		},
		{
			URL:                 backend2.URL,
			HealthCheckEndpoint: "/health",
		},
	}

	hc := NewHealthChecker(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hc.Start(ctx, backends)

	time.Sleep(200 * time.Millisecond)

	status1 := hc.GetHealthStatus(backend1.URL)
	if status1 == nil {
		t.Fatal("expected health status for backend1")
	}
	if !status1.IsHealthy {
		t.Errorf("expected backend1 to be healthy, got false")
	}

	status2 := hc.GetHealthStatus(backend2.URL)
	if status2 == nil {
		t.Fatal("expected health status for backend2")
	}
	if status2.IsHealthy {
		t.Errorf("expected backend2 to be unhealthy, got true")
	}
}

func TestHealthChecker_ConcurrentAccess(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer backend.Close()

	backends := []utils.BackendConfig{
		{
			URL:                 backend.URL,
			HealthCheckEndpoint: "/health",
		},
	}

	hc := NewHealthChecker(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hc.Start(ctx, backends)

	time.Sleep(100 * time.Millisecond)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			status := hc.GetHealthStatus(backend.URL)
			done <- (status != nil)
		}()
	}

	for i := 0; i < 10; i++ {
		if !<-done {
			t.Error("concurrent GetHealthStatus call returned nil")
		}
	}
}

func TestHealthChecker_OnlyHealthEndpoint(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer backend.Close()

	backends := []utils.BackendConfig{
		{
			URL:                 backend.URL,
			HealthCheckEndpoint: "/health",
		},
	}

	hc := NewHealthChecker(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hc.Start(ctx, backends)

	time.Sleep(200 * time.Millisecond)

	status := hc.GetHealthStatus(backend.URL)
	if status == nil {
		t.Fatal("expected health status to exist")
	}

	if !status.IsHealthy {
		t.Errorf("expected backend to be healthy, got false")
	}

	if !status.IsReady {
		t.Errorf("expected backend to be ready by default when no readiness endpoint, got false")
	}
}

func BenchmarkHealthChecker_GetHealthStatus(b *testing.B) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer backend.Close()

	backends := []utils.BackendConfig{
		{
			URL:                 backend.URL,
			HealthCheckEndpoint: "/health",
		},
	}

	hc := NewHealthChecker(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hc.Start(ctx, backends)

	time.Sleep(200 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hc.GetHealthStatus(backend.URL)
	}
}
