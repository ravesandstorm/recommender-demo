package testsuite

import (
	"context"
	"fmt"
	"time"
)

// Scope defines the functional domain or test type.
type Scope string

const (
	ScopeUnitVector     Scope = "Unit/Vector"
	ScopeUnitHandlers   Scope = "Unit/Handlers"
	ScopeUnitWorkers    Scope = "Unit/Workers"
	ScopeRedisStore     Scope = "Redis/Store"
	ScopePostgresDB     Scope = "Postgres/DB"
	ScopeHTTPAPI        Scope = "HTTP/API"
	ScopeIntegrationE2E Scope = "Integration/E2E"
	ScopeEdgeCases      Scope = "Edge/Boundary"
)

// Status represents the outcome of a test.
type Status string

const (
	StatusPass Status = "PASS"
	StatusFail Status = "FAIL"
	StatusSkip Status = "SKIP"
)

// EdgeCaseTag classifies the specific edge condition being exercised.
type EdgeCaseTag string

const (
	EdgeZeroOrNilVector      EdgeCaseTag = "Zero / Nil Vector Input"
	EdgeCollinearInterests   EdgeCaseTag = "Collinear Centroids Collapse"
	EdgeMaxKCentroidsCap     EdgeCaseTag = "Max K Centroids Capping"
	EdgeNegativeMassPrune    EdgeCaseTag = "Centroid Pruning (Weight <= 0)"
	EdgeMMRLambdaExtremes    EdgeCaseTag = "MMR Lambda Extremes (0.0 & 1.0)"
	EdgeQueueSaturationDrop  EdgeCaseTag = "Async Worker Queue Saturation"
	EdgeSeenHydrateFallback  EdgeCaseTag = "Seen Filter Cold-Start Hydration"
	EdgeCascadeUserCleanup   EdgeCaseTag = "Cascade User & Engagement Deletion"
	EdgeViewRetainLimitTrim  EdgeCaseTag = "View Impression Retention Pruning"
	EdgeShareMemoDeduplicate EdgeCaseTag = "Share Preference Deduplication"
	EdgeTombstoneResolution  EdgeCaseTag = "Redis Tombstone 'x' Overwrites"
	EdgeAuthValidation       EdgeCaseTag = "Missing / Invalid Auth Headers"
	EdgeEmptyFeedPool        EdgeCaseTag = "Exhausted Unseen Feed Fallback"
	EdgeRapidToggleCycle     EdgeCaseTag = "Rapid Interaction Toggling"
)

// Metrics records performance and memory stats for a single test.
type Metrics struct {
	Duration   time.Duration `json:"duration_ns"`
	DurationMs float64       `json:"duration_ms"`
	AllocBytes uint64        `json:"alloc_bytes"`
	AllocCount uint64        `json:"alloc_count"`
	Assertions int           `json:"assertions"`
}

// TestResult encapsulates the outcome and telemetry of a test execution.
type TestResult struct {
	Name        string        `json:"name"`
	Scope       Scope         `json:"scope"`
	EdgeCase    EdgeCaseTag   `json:"edge_case,omitempty"`
	Description string        `json:"description"`
	Status      Status        `json:"status"`
	Metrics     Metrics       `json:"metrics"`
	Error       string        `json:"error,omitempty"`
	Logs        []string      `json:"logs,omitempty"`
}

// TestContext provides assertion helpers and logging for test functions.
type TestContext struct {
	ctx        context.Context
	assertions int
	logs       []string
	failed     bool
	errMsg     string
	skipped    bool
	skipReason string
}

func NewTestContext(ctx context.Context) *TestContext {
	return &TestContext{
		ctx: ctx,
	}
}

func (tc *TestContext) Context() context.Context {
	if tc.ctx == nil {
		return context.Background()
	}
	return tc.ctx
}

func (tc *TestContext) Log(format string, args ...any) {
	tc.logs = append(tc.logs, fmt.Sprintf(format, args...))
}

func (tc *TestContext) Assert(condition bool, message string, args ...any) bool {
	tc.assertions++
	if !condition {
		tc.failed = true
		tc.errMsg = fmt.Sprintf(message, args...)
		return false
	}
	return true
}

func (tc *TestContext) AssertEqual(expected, actual any, name string) bool {
	tc.assertions++
	expStr := fmt.Sprintf("%v", expected)
	actStr := fmt.Sprintf("%v", actual)
	if expStr != actStr {
		tc.failed = true
		tc.errMsg = fmt.Sprintf("%s: expected '%s', got '%s'", name, expStr, actStr)
		return false
	}
	return true
}

func (tc *TestContext) AssertNoError(err error, op string) bool {
	tc.assertions++
	if err != nil {
		tc.failed = true
		tc.errMsg = fmt.Sprintf("%s failed with error: %v", op, err)
		return false
	}
	return true
}

func (tc *TestContext) AssertError(err error, op string) bool {
	tc.assertions++
	if err == nil {
		tc.failed = true
		tc.errMsg = fmt.Sprintf("%s expected an error but got nil", op)
		return false
	}
	return true
}

func (tc *TestContext) Skip(reason string) {
	tc.skipped = true
	tc.skipReason = reason
}

// TestFunc is the signature for test implementation functions.
type TestFunc func(tc *TestContext)

// TestCase represents an individual runnable test with metadata.
type TestCase struct {
	Name        string
	Scope       Scope
	EdgeCase    EdgeCaseTag
	Description string
	RequiresInfra bool // true if test requires Redis/Postgres/Qdrant/FastEmbed
	Fn          TestFunc
}

// ScopeSummary holds aggregated metrics for a specific scope.
type ScopeSummary struct {
	Scope        Scope         `json:"scope"`
	Total        int           `json:"total"`
	Passed       int           `json:"passed"`
	Failed       int           `json:"failed"`
	Skipped      int           `json:"skipped"`
	TotalTime    time.Duration `json:"total_time_ns"`
	AvgDurationMs float64      `json:"avg_duration_ms"`
	MaxDurationMs float64      `json:"max_duration_ms"`
	TotalAllocs  uint64        `json:"total_allocs"`
	TotalBytes   uint64        `json:"total_bytes"`
}

// EdgeCaseSummary holds coverage counts for edge case tags.
type EdgeCaseSummary struct {
	Tag     EdgeCaseTag `json:"tag"`
	Total   int         `json:"total"`
	Passed  int         `json:"passed"`
	Failed  int         `json:"failed"`
	Skipped int         `json:"skipped"`
}

// SuiteSummary is the full aggregate result of the test run.
type SuiteSummary struct {
	TotalTests    int                        `json:"total_tests"`
	Passed        int                        `json:"passed"`
	Failed        int                        `json:"failed"`
	Skipped       int                        `json:"skipped"`
	PassRate      float64                    `json:"pass_rate_pct"`
	TotalDuration time.Duration              `json:"total_duration_ns"`
	TotalAllocs   uint64                     `json:"total_allocs"`
	TotalBytes    uint64                     `json:"total_bytes"`
	ScopeStats    map[Scope]*ScopeSummary    `json:"scope_stats"`
	EdgeStats     map[EdgeCaseTag]*EdgeCaseSummary `json:"edge_stats"`
	Results       []TestResult               `json:"results"`
}
