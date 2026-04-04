package constants

// Rate Limiter Constants
const (
	RateLimitPrefix = "ratelimit:"
)

// Circuit Breaker Constants
const (
	CounterTypeRingBuffer   = "ringbuffer"
	CounterTypeSlidingWindow = "sliding_window"
)

// Rate Limiter Types
const (
	RateLimiterTypeSlidingWindow = "sliding_window"
	RateLimiterTypeTokenBucket   = "token_bucket"
)

// Error Messages
const (
	ErrCircuitBreakerOpenUser     = "Service temporarily unavailable"
	ErrCircuitBreakerOpenDev      = "circuit breaker is open for backend"
	ErrRateLimitExceededUser      = "Too many requests"
	ErrRateLimitExceededDev       = "rate limit exceeded for backend"
	ErrInternalErrorUser          = "Internal server error"
	ErrInternalErrorDev           = "internal error occurred"
)

// Test Scenarios
const (
	TestScenarioNormalProxy       = "Normal proxy operation with healthy backend"
	TestScenarioUnhealthyBackend  = "Backend returns error"
	TestScenarioCircuitOpen       = "Circuit breaker is open"
	TestScenarioRateLimitExceeded = "Rate limit exceeded"
	TestScenarioHalfOpen          = "Half-open state with high traffic"
	TestScenarioConfigJSON        = "Load configuration from JSON"
	TestScenarioConfigYAML        = "Load configuration from YAML"
)

// Headers to block from forwarding
var BlockedHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

// Headers to allow forwarding (if empty, all non-blocked headers are allowed)
var AllowedHeaders = []string{
	"Accept",
	"Accept-Encoding",
	"Accept-Language",
	"Authorization",
	"Cache-Control",
	"Content-Type",
	"Date",
	"If-Match",
	"If-Modified-Since",
	"If-None-Match",
	"If-Range",
	"If-Unmodified-Since",
	"Origin",
	"Range",
	"Referer",
	"User-Agent",
}