package usecase

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"runtime/debug"

	"golang.org/x/sync/singleflight"
	"goproxy/pkg/constants"
	"goproxy/pkg/errors"
	"goproxy/pkg/metrics"
)

// ProxyUsecase defines the interface for proxy operations
type ProxyUsecase interface {
	ForwardRequest(w http.ResponseWriter, r *http.Request, backendURL, endpoint string) error
}

// ProxyResponse holds the response data for singleflight
type ProxyResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// HTTPProxy implements ProxyUsecase
type HTTPProxy struct {
	cbManager          CircuitBreakerUsecase
	rlManager          RateLimiterUsecase
	sf                 singleflight.Group
	enableSingleflight bool
}

// NewHTTPProxy creates a new HTTPProxy
func NewHTTPProxy(cbManager CircuitBreakerUsecase, rlManager RateLimiterUsecase, enableSingleflight bool) *HTTPProxy {
	return &HTTPProxy{
		cbManager:          cbManager,
		rlManager:          rlManager,
		enableSingleflight: enableSingleflight,
	}
}

// ForwardRequest forwards the request to the backend
func (p *HTTPProxy) ForwardRequest(w http.ResponseWriter, r *http.Request, backendURL, endpoint string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic in ForwardRequest: %v\nStack trace:\n%s", r, debug.Stack())
			err = fmt.Errorf("internal panic: %v", r)
		}
	}()
	// Check rate limit first
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

	if !p.cbManager.CanExecute(backendURL) {
		metrics.TrafficBlocked.WithLabelValues(backendURL, endpoint, "circuit_breaker").Inc()
		http.Error(w, constants.ErrCircuitBreakerOpenUser, http.StatusServiceUnavailable)
		return errors.NewCircuitBreakerOpenError(backendURL, nil)
	}

	// Use singleflight only for GET requests if enabled
	if p.enableSingleflight && r.Method == "GET" {
		// Key for singleflight: combine backend and request path to deduplicate identical requests
		key := backendURL + r.URL.Path

		// Use singleflight to avoid duplicate backend calls
		result, err, _ := p.sf.Do(key, func() (interface{}, error) {
			return p.doRequest(r, backendURL)
		})
		if err != nil {
			return err
		}

		proxyResp := result.(*ProxyResponse)

		// Write response
		for k, v := range proxyResp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(proxyResp.StatusCode)
		w.Write(proxyResp.Body)

		return nil
	}

	// Normal request without singleflight
	proxyResp, err := p.doRequest(r, backendURL)
	if err != nil {
		return err
	}

	// Write response
	for k, v := range proxyResp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(proxyResp.StatusCode)
	w.Write(proxyResp.Body)

	return nil
}

// doRequest performs the actual HTTP request
func (p *HTTPProxy) doRequest(r *http.Request, backendURL string) (resp *ProxyResponse, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic in doRequest: %v\nStack trace:\n%s", r, debug.Stack())
			err = fmt.Errorf("internal panic: %v", r)
		}
	}()
	// Parse backend URL
	target, err := url.Parse(backendURL)
	if err != nil {
		p.cbManager.RecordFailure(backendURL)
		return nil, errors.NewInternalError("failed to parse backend URL", err)
	}

	// Create new request
	req, err := http.NewRequest(r.Method, target.String()+r.URL.Path, r.Body)
	if err != nil {
		p.cbManager.RecordFailure(backendURL)
		return nil, errors.NewInternalError("failed to create request", err)
	}

	// Copy headers
	for k, v := range r.Header {
		req.Header[k] = v
	}

	// Execute request
	client := &http.Client{}
	httpResp, err := client.Do(req)
	if err != nil {
		p.cbManager.RecordFailure(backendURL)
		return nil, errors.NewInternalError("failed to execute request", err)
	}
	defer httpResp.Body.Close()

	// Read body
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		p.cbManager.RecordFailure(backendURL)
		return nil, errors.NewInternalError("failed to read response body", err)
	}

	// Check status
	if httpResp.StatusCode >= 500 {
		p.cbManager.RecordFailure(backendURL)
	} else {
		p.cbManager.RecordSuccess(backendURL)
		// Note: endpoint is not available here, so we can't record per endpoint success
		// For now, record with empty endpoint
		metrics.TrafficSuccess.WithLabelValues(backendURL, "").Inc()
	}

	return &ProxyResponse{
		StatusCode: httpResp.StatusCode,
		Header:     httpResp.Header,
		Body:       body,
	}, nil
}