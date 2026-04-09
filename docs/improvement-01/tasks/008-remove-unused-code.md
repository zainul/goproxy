# Task 008: Remove Unused Code (InMemoryMetricsRepository)

**Priority**: P2 (Medium)  
**Estimated Complexity**: Low  
**Files to Modify**: 
- `internal/repository/metrics.go` (DELETE or keep for future)

---

## Problem Statement

The `InMemoryMetricsRepository` in `internal/repository/metrics.go` is fully implemented but **never used** in the codebase:

- Interface defined: `MetricsRepository`
- Implementation exists: `InMemoryMetricsRepository`
- Usage in `main.go`: **None**
- Usage anywhere else: **None**

This is dead code that adds maintenance burden.

---

## Solution Options

### Option A: Delete the File (Recommended if not needed soon)
Remove the file entirely to reduce codebase size.

### Option B: Keep for Future Use (Recommended if metrics storage is planned)
Add a `// TODO` comment explaining why it exists and when to use it.

---

## Step-by-Step Instructions (Option B - Keep with Documentation)

### Step 1: Add Documentation Comment

**File**: `internal/repository/metrics.go`

**ADD** at the top of the file (after package declaration):

```go
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
```

---

### Step 2: Verify No Imports Break

```bash
# Check if metrics.go is imported anywhere
grep -r "InMemoryMetricsRepository" --include="*.go" .
grep -r "MetricsRepository" --include="*.go" .

# Should only find references in metrics.go itself
```

---

### Step 3: Run Tests

```bash
# Ensure no tests depend on this code
go test ./...

# Verify build still works
go build ./...
```

---

## Verification Checklist

- [ ] Documentation comment added explaining unused status
- [ ] No other files import or use InMemoryMetricsRepository
- [ ] All tests pass: `go test ./...`
- [ ] Build succeeds: `go build ./...`

---

## Decision Matrix

| Scenario | Action |
|----------|--------|
| No plans to use in-memory metrics | Delete file |
| Planning to add metrics dashboard | Keep with TODO |
| Need it for testing | Keep and add tests |
| Unsure | Keep with documentation comment |

---

## Success Criteria

1. Dead code is documented or removed
2. No build or test failures
3. Clear guidance for future developers
