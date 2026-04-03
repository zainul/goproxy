# Task 009: Add Middleware Panic Recovery Tests

**Priority**: P1 (High)  
**Estimated Complexity**: Low  
**Files to Create**: 
- `pkg/middleware/middleware_test.go`

---

## Problem Statement

The panic recovery middleware in `pkg/middleware/middleware.go` has **zero test coverage**. This is critical because:
- Panic recovery is the last line of defense against crashes
- Incorrect recovery could mask bugs or cause data corruption
- Stack trace logging needs verification

---

## Solution

Create tests that verify panic recovery behavior.

---

## Step-by-Step Instructions

### Step 1: Create Test File

**File**: `pkg/middleware/middleware_test.go` (CREATE NEW FILE)

**Add imports**:
```go
package middleware

import (
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/stretchr/testify/assert"
)
```

---

### Step 2: Write Test for Panic Recovery

**ADD** this test function:

```go
func TestPanicRecovery_RecoversFromPanic(t *testing.T) {
    // Create a handler that panics
    panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        panic("test panic")
    })

    // Wrap with panic recovery middleware
    handler := PanicRecovery(panicHandler)

    // Create test request
    req := httptest.NewRequest(http.MethodGet, "/test", nil)
    rr := httptest.NewRecorder()

    // Execute - should not panic
    assert.NotPanics(t, func() {
        handler.ServeHTTP(rr, req)
    })

    // Should return 500 Internal Server Error
    assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestPanicRecovery_PassesNormalRequests(t *testing.T) {
    // Create a normal handler
    normalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("OK"))
    })

    // Wrap with middleware
    handler := PanicRecovery(normalHandler)

    // Create test request
    req := httptest.NewRequest(http.MethodGet, "/test", nil)
    rr := httptest.NewRecorder()

    // Execute
    handler.ServeHTTP(rr, req)

    // Should pass through normally
    assert.Equal(t, http.StatusOK, rr.Code)
    assert.Equal(t, "OK", rr.Body.String())
}

func TestPanicRecovery_Returns500OnPanic(t *testing.T) {
    // Create a handler that panics
    panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        panic("intentional panic")
    })

    handler := PanicRecovery(panicHandler)

    req := httptest.NewRequest(http.MethodGet, "/test", nil)
    rr := httptest.NewRecorder()

    handler.ServeHTTP(rr, req)

    // Verify 500 status code
    assert.Equal(t, http.StatusInternalServerError, rr.Code)
}
```

---

### Step 3: Run Tests

```bash
# Run middleware tests
go test ./pkg/middleware -v

# Run all tests
go test ./...

# Check for race conditions
go test -race ./pkg/middleware
```

---

## Verification Checklist

- [ ] `pkg/middleware/middleware_test.go` created
- [ ] Test for panic recovery
- [ ] Test for normal request passthrough
- [ ] Test for 500 status code on panic
- [ ] All tests pass: `go test ./...`
- [ ] No race conditions: `go test -race ./...`

---

## Success Criteria

1. Panic recovery middleware is tested
2. Normal requests pass through unchanged
3. Panics are caught and return 500
4. No test failures or race conditions
