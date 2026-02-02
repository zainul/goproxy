package usecase

import (
	"fmt"
	"io"
	"net/http"
	"net/url"

	"golang.org/x/sync/singleflight"
)

// ProxyUsecase defines the interface for proxy operations
type ProxyUsecase interface {
	ForwardRequest(w http.ResponseWriter, r *http.Request, backendURL string) error
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
	sf                 singleflight.Group
	enableSingleflight bool
}

// NewHTTPProxy creates a new HTTPProxy
func NewHTTPProxy(cbManager CircuitBreakerUsecase, enableSingleflight bool) *HTTPProxy {
	return &HTTPProxy{
		cbManager:          cbManager,
		enableSingleflight: enableSingleflight,
	}
}

// ForwardRequest forwards the request to the backend
func (p *HTTPProxy) ForwardRequest(w http.ResponseWriter, r *http.Request, backendURL string) error {
	if !p.cbManager.CanExecute(backendURL) {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return fmt.Errorf("circuit breaker open for %s", backendURL)
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
func (p *HTTPProxy) doRequest(r *http.Request, backendURL string) (*ProxyResponse, error) {
	// Parse backend URL
	target, err := url.Parse(backendURL)
	if err != nil {
		p.cbManager.RecordFailure(backendURL)
		return nil, err
	}

	// Create new request
	req, err := http.NewRequest(r.Method, target.String()+r.URL.Path, r.Body)
	if err != nil {
		p.cbManager.RecordFailure(backendURL)
		return nil, err
	}

	// Copy headers
	for k, v := range r.Header {
		req.Header[k] = v
	}

	// Execute request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		p.cbManager.RecordFailure(backendURL)
		return nil, err
	}
	defer resp.Body.Close()

	// Read body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.cbManager.RecordFailure(backendURL)
		return nil, err
	}

	// Check status
	if resp.StatusCode >= 500 {
		p.cbManager.RecordFailure(backendURL)
	} else {
		p.cbManager.RecordSuccess(backendURL)
	}

	return &ProxyResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       body,
	}, nil
}