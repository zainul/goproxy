package usecase

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"goproxy/internal/entity"
	"goproxy/pkg/constants"
	"goproxy/pkg/errors"
	"goproxy/pkg/metrics"
	"goproxy/pkg/utils"

	"golang.org/x/sync/singleflight"
)

// ProxyUsecase defines the interface for proxy operations
type ProxyUsecase interface {
	ForwardRequest(w http.ResponseWriter, r *http.Request, endpoint string) error
}

// ProxyResponse holds the response data for singleflight
type ProxyResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func isHeaderAllowed(header string) bool {
	for _, blocked := range constants.BlockedHeaders {
		if strings.EqualFold(header, blocked) {
			return false
		}
	}

	if len(constants.AllowedHeaders) == 0 {
		return true
	}

	for _, allowed := range constants.AllowedHeaders {
		if strings.EqualFold(header, allowed) {
			return true
		}
	}

	return false
}

// HTTPProxy implements ProxyUsecase
type HTTPProxy struct {
	cbManager          CircuitBreakerUsecase
	rlManager          RateLimiterUsecase
	lb                 *entity.LoadBalancer
	sf                 singleflight.Group
	enableSingleflight bool
	timeoutByBackend   map[string]time.Duration // Per-backend request propagation timeouts
	httpClient         *http.Client
}

// NewHTTPProxy creates a new HTTPProxy
func NewHTTPProxy(cbManager CircuitBreakerUsecase, rlManager RateLimiterUsecase, lb *entity.LoadBalancer, enableSingleflight bool, timeout time.Duration, transport *utils.TransportConfig) *HTTPProxy {
	idleConnTimeout, _ := time.ParseDuration(transport.IdleConnTimeout)
	tlsHandshakeTimeout, _ := time.ParseDuration(transport.TLSHandshakeTimeout)
	responseHeaderTimeout, _ := time.ParseDuration(transport.ResponseHeaderTimeout)
	expectContinueTimeout, _ := time.ParseDuration(transport.ExpectContinueTimeout)

	t := &http.Transport{
		MaxIdleConns:          transport.MaxIdleConns,
		MaxIdleConnsPerHost:   transport.MaxIdleConnsPerHost,
		MaxConnsPerHost:       transport.MaxConnsPerHost,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		ExpectContinueTimeout: expectContinueTimeout,
		DisableKeepAlives:     transport.DisableKeepAlives,
		DisableCompression:    transport.DisableCompression,
		WriteBufferSize:       transport.WriteBufferSize,
		ReadBufferSize:        transport.ReadBufferSize,
		ForceAttemptHTTP2:     true,
	}

	proxy := &HTTPProxy{
		cbManager:          cbManager,
		rlManager:          rlManager,
		lb:                 lb,
		sf:                 singleflight.Group{},
		enableSingleflight: enableSingleflight,
		timeoutByBackend:   nil, // Per-backend timeouts set after initialization
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: t,
		},
	}

	return proxy
}

// SetBackendTimeout sets a per-backend timeout for request propagation
func (p *HTTPProxy) SetBackendTimeout(backendURL string, timeout time.Duration) {
	p.timeoutByBackend = make(map[string]time.Duration)
	p.timeoutByBackend[backendURL] = timeout
}

// getBackendTimeout retrieves the timeout for a backend, falling back to global default
func (p *HTTPProxy) getBackendTimeout(backendURL string) time.Duration {
	if p.timeoutByBackend != nil {
		if t, ok := p.timeoutByBackend[backendURL]; ok {
			return t
		}
	}
	return p.httpClient.Timeout
}

// ForwardRequest forwards the request to the backend
func (p *HTTPProxy) ForwardRequest(w http.ResponseWriter, r *http.Request, endpoint string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic in ForwardRequest: %v\nStack trace:\n%s", r, debug.Stack())
			err = fmt.Errorf("internal panic: %v", r)
		}
	}()

	backend := p.lb.NextHealthyBackend()
	if backend == nil {
		metrics.TrafficBlocked.WithLabelValues("", endpoint, "no_backend").Inc()
		http.Error(w, "No backend available", http.StatusServiceUnavailable)
		return fmt.Errorf("no backend available")
	}
	backendURL := backend.URL

	// Check backend-wide rate limit first
	allowed, err := p.rlManager.Allow(r.Context(), backendURL)
	if err != nil {
		metrics.TrafficBlocked.WithLabelValues(backendURL, endpoint, "rate_limit_error").Inc()
		appErr, ok := err.(*errors.AppError)
		if ok {
			http.Error(w, appErr.UserError(), http.StatusInternalServerError)
		} else {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return err
	}
	if !allowed {
		metrics.TrafficBlocked.WithLabelValues(backendURL, endpoint, "rate_limit").Inc()
		metrics.RateLimitReached.WithLabelValues(backendURL, endpoint).Inc()
		http.Error(w, constants.ErrRateLimitExceededUser, http.StatusTooManyRequests)
		return errors.NewRateLimitExceededError(backendURL, nil)
	}

	// Check endpoint-specific rate limit
	allowed, err = p.rlManager.AllowEndpoint(r.Context(), backendURL, endpoint)
	if err != nil {
		metrics.TrafficBlocked.WithLabelValues(backendURL, endpoint, "endpoint_rate_limit_error").Inc()
		appErr, ok := err.(*errors.AppError)
		if ok {
			http.Error(w, appErr.UserError(), http.StatusInternalServerError)
		} else {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return err
	}
	if !allowed {
		metrics.TrafficBlocked.WithLabelValues(backendURL, endpoint, "endpoint_rate_limit").Inc()
		metrics.RateLimitReached.WithLabelValues(backendURL, endpoint).Inc()
		http.Error(w, constants.ErrRateLimitExceededUser, http.StatusTooManyRequests)
		return errors.NewRateLimitExceededError(backendURL+endpoint, nil)
	}

	if !p.cbManager.CanExecute(backendURL) {
		metrics.TrafficBlocked.WithLabelValues(backendURL, endpoint, "circuit_breaker").Inc()
		http.Error(w, constants.ErrCircuitBreakerOpenUser, http.StatusServiceUnavailable)
		return errors.NewCircuitBreakerOpenError(backendURL, nil)
	}

	// Wrap request context with per-backend timeout for request propagation
	ctx := r.Context()
	timeout := p.getBackendTimeout(backendURL)
	if timeout > 0 {
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Use singleflight only for GET requests if enabled
	if p.enableSingleflight && r.Method == "GET" {
		key := backendURL + r.URL.Path

		result, err, _ := p.sf.Do(key, func() (interface{}, error) {
			return p.doRequest(r, backendURL, endpoint)
		})
		if err != nil {
			return err
		}

		proxyResp := result.(*ProxyResponse)

		for k, v := range proxyResp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(proxyResp.StatusCode)
		w.Write(proxyResp.Body)

		return nil
	}

	proxyResp, err := p.doRequest(r, backendURL, endpoint)
	if err != nil {
		return err
	}

	for k, v := range proxyResp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(proxyResp.StatusCode)
	w.Write(proxyResp.Body)

	return nil
}

// doRequest performs the actual HTTP request
func (p *HTTPProxy) doRequest(r *http.Request, backendURL, endpoint string) (resp *ProxyResponse, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic in doRequest: %v\nStack trace:\n%s", r, debug.Stack())
			err = fmt.Errorf("internal panic: %v", r)
		}
	}()
	target, err := url.Parse(backendURL)
	if err != nil {
		p.cbManager.RecordFailure(backendURL)
		return nil, errors.NewInternalError("failed to parse backend URL", err)
	}

	req, err := http.NewRequest(r.Method, target.String()+r.URL.Path, r.Body)
	if err != nil {
		p.cbManager.RecordFailure(backendURL)
		return nil, errors.NewInternalError("failed to create request", err)
	}

	for k, v := range r.Header {
		if !isHeaderAllowed(k) {
			continue
		}
		req.Header[k] = v
	}

	httpResp, err := p.httpClient.Do(req)
	if err != nil {
		p.cbManager.RecordFailure(backendURL)
		return nil, errors.NewInternalError("failed to execute request", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		p.cbManager.RecordFailure(backendURL)
		return nil, errors.NewInternalError("failed to read response body", err)
	}

	if httpResp.StatusCode >= 500 {
		p.cbManager.RecordFailure(backendURL)
	} else {
		p.cbManager.RecordSuccess(backendURL)
		metrics.TrafficSuccess.WithLabelValues(backendURL, endpoint).Inc()
	}

	return &ProxyResponse{
		StatusCode: httpResp.StatusCode,
		Header:     httpResp.Header,
		Body:       body,
	}, nil
}
