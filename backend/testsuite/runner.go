package testsuite

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/recsys/backend/internal/config"
	"github.com/recsys/backend/internal/db"
	"github.com/recsys/backend/internal/embedclient"
	"github.com/recsys/backend/internal/migrate"
	"github.com/recsys/backend/internal/qdrantclient"
	"github.com/recsys/backend/internal/redisstore"
)

// Runner orchestrates test registration, execution, metrics measurement, and reporting.
type Runner struct {
	cfg        config.Config
	cases      []TestCase
	reporter   *Reporter
	infraReady bool
	dbStore    *db.Store
	redisStore *redisstore.Store
	qdrant     *qdrantclient.Client
	embed      *embedclient.Client
}

func NewRunner(cfg config.Config, reporter *Reporter) *Runner {
	return &Runner{
		cfg:      cfg,
		cases:    make([]TestCase, 0),
		reporter: reporter,
	}
}

func (r *Runner) Register(tc TestCase) {
	r.cases = append(r.cases, tc)
}

func (r *Runner) RegisterBatch(cases []TestCase) {
	r.cases = append(r.cases, cases...)
}

func (r *Runner) InitInfra(ctx context.Context) error {
	// Probe health of all dependencies
	ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// 1. Redis
	rdb := redisstore.New(r.cfg.RedisAddr, r.cfg.VectorDim)
	if err := rdb.Ping(ctxTimeout); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}
	r.redisStore = rdb

	// 2. Postgres
	pool, err := pgxpool.New(ctxTimeout, r.cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("postgres dial failed: %w", err)
	}
	if err := pool.Ping(ctxTimeout); err != nil {
		pool.Close()
		return fmt.Errorf("postgres ping failed: %w", err)
	}
	store := &db.Store{Pool: pool}
	if err := migrate.Up(ctxTimeout, pool); err != nil {
		pool.Close()
		return fmt.Errorf("postgres migrations failed: %w", err)
	}
	r.dbStore = store

	// 3. Qdrant
	q := qdrantclient.New(r.cfg.QdrantURL, r.cfg.QdrantCollection, r.cfg.VectorDim)
	if err := q.EnsureCollection(ctxTimeout); err != nil {
		return fmt.Errorf("qdrant ensure collection failed: %w", err)
	}
	r.qdrant = q

	// 4. FastEmbed
	emb := embedclient.New(r.cfg.EmbeddingURL)
	if err := pingURL(ctxTimeout, r.cfg.EmbeddingURL+"/health"); err != nil {
		return fmt.Errorf("embedding health check failed: %w", err)
	}
	r.embed = emb

	r.infraReady = true
	return nil
}

func (r *Runner) Close() {
	if r.dbStore != nil && r.dbStore.Pool != nil {
		r.dbStore.Pool.Close()
	}
}

func pingURL(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// Run executes all tests that match the scope filter and test name pattern.
func (r *Runner) Run(ctx context.Context, scopeFilter, namePattern string) (*SuiteSummary, error) {
	// Filter test cases
	filtered := make([]TestCase, 0, len(r.cases))
	for _, tc := range r.cases {
		if scopeFilter != "" && scopeFilter != "all" {
			if !matchesScope(tc.Scope, scopeFilter) {
				continue
			}
		}
		if namePattern != "" {
			if !strings.Contains(strings.ToLower(tc.Name), strings.ToLower(namePattern)) &&
				!strings.Contains(strings.ToLower(tc.Description), strings.ToLower(namePattern)) {
				continue
			}
		}
		filtered = append(filtered, tc)
	}

	r.reporter.PrintHeader(scopeFilter, len(filtered))

	summary := &SuiteSummary{
		TotalTests: len(filtered),
		ScopeStats: make(map[Scope]*ScopeSummary),
		EdgeStats:  make(map[EdgeCaseTag]*EdgeCaseSummary),
		Results:    make([]TestResult, 0, len(filtered)),
	}

	totalStart := time.Now()

	for _, tc := range filtered {
		res := r.runSingle(ctx, tc)
		r.reporter.PrintResult(res)
		summary.Results = append(summary.Results, res)

		// Aggregate into scope summary
		scStat, ok := summary.ScopeStats[res.Scope]
		if !ok {
			scStat = &ScopeSummary{Scope: res.Scope}
			summary.ScopeStats[res.Scope] = scStat
		}
		scStat.Total++
		scStat.TotalTime += res.Metrics.Duration
		scStat.TotalAllocs += res.Metrics.AllocCount
		scStat.TotalBytes += res.Metrics.AllocBytes
		if res.Metrics.DurationMs > scStat.MaxDurationMs {
			scStat.MaxDurationMs = res.Metrics.DurationMs
		}

		// Aggregate into edge case summary
		if res.EdgeCase != "" {
			esStat, ok := summary.EdgeStats[res.EdgeCase]
			if !ok {
				esStat = &EdgeCaseSummary{Tag: res.EdgeCase}
				summary.EdgeStats[res.EdgeCase] = esStat
			}
			esStat.Total++
			switch res.Status {
			case StatusPass:
				esStat.Passed++
			case StatusFail:
				esStat.Failed++
			case StatusSkip:
				esStat.Skipped++
			}
		}

		switch res.Status {
		case StatusPass:
			summary.Passed++
			scStat.Passed++
		case StatusFail:
			summary.Failed++
			scStat.Failed++
		case StatusSkip:
			summary.Skipped++
			scStat.Skipped++
		}

		summary.TotalAllocs += res.Metrics.AllocCount
		summary.TotalBytes += res.Metrics.AllocBytes
	}

	summary.TotalDuration = time.Since(totalStart)
	if summary.TotalTests > 0 {
		activeCount := summary.Passed + summary.Failed
		if activeCount > 0 {
			summary.PassRate = (float64(summary.Passed) / float64(activeCount)) * 100.0
		}
	}

	// Calculate average latencies per scope
	for _, scStat := range summary.ScopeStats {
		if scStat.Total > 0 {
			scStat.AvgDurationMs = float64(scStat.TotalTime.Microseconds()) / (float64(scStat.Total) * 1000.0)
		}
	}

	r.reporter.PrintSummary(summary)
	return summary, nil
}

func (r *Runner) runSingle(ctx context.Context, tc TestCase) (res TestResult) {
	res = TestResult{
		Name:        tc.Name,
		Scope:       tc.Scope,
		EdgeCase:    tc.EdgeCase,
		Description: tc.Description,
	}

	// Check if infra is needed but not ready
	if tc.RequiresInfra && !r.infraReady {
		res.Status = StatusSkip
		res.Error = "Requires live Docker infrastructure"
		return res
	}

	tctx := NewTestContext(ctx)

	// Memory & allocation baseline
	var memBefore, memAfter runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)
	start := time.Now()

	// Safe execution with panic handler
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				tctx.failed = true
				tctx.errMsg = fmt.Sprintf("panic recovered: %v", rec)
			}
		}()
		tc.Fn(tctx)
	}()

	duration := time.Since(start)
	runtime.ReadMemStats(&memAfter)

	// Calculate metrics
	var allocBytes uint64
	var allocCount uint64
	if memAfter.TotalAlloc >= memBefore.TotalAlloc {
		allocBytes = memAfter.TotalAlloc - memBefore.TotalAlloc
	}
	if memAfter.Mallocs >= memBefore.Mallocs {
		allocCount = memAfter.Mallocs - memBefore.Mallocs
	}

	res.Metrics = Metrics{
		Duration:   duration,
		DurationMs: float64(duration.Microseconds()) / 1000.0,
		AllocBytes: allocBytes,
		AllocCount: allocCount,
		Assertions: tctx.assertions,
	}
	res.Logs = tctx.logs

	if tctx.skipped {
		res.Status = StatusSkip
		res.Error = tctx.skipReason
	} else if tctx.failed {
		res.Status = StatusFail
		res.Error = tctx.errMsg
	} else {
		res.Status = StatusPass
	}

	return res
}

func matchesScope(scope Scope, filter string) bool {
	f := strings.ToLower(filter)
	s := strings.ToLower(string(scope))
	if f == "all" {
		return true
	}
	if f == "unit" {
		return strings.HasPrefix(s, "unit/")
	}
	if f == "integration" {
		return strings.HasPrefix(s, "integration/")
	}
	if f == "edge" || f == "edge-case" || f == "edge_case" {
		return strings.HasPrefix(s, "edge/")
	}
	return strings.Contains(s, f)
}

// GetDB returns the initialized db store.
func (r *Runner) GetDB() *db.Store {
	return r.dbStore
}

// GetRedis returns the initialized redis store.
func (r *Runner) GetRedis() *redisstore.Store {
	return r.redisStore
}

// GetQdrant returns the initialized Qdrant client.
func (r *Runner) GetQdrant() *qdrantclient.Client {
	return r.qdrant
}

// GetEmbed returns the initialized fastembed client.
func (r *Runner) GetEmbed() *embedclient.Client {
	return r.embed
}

// GetConfig returns the runner config.
func (r *Runner) GetConfig() config.Config {
	return r.cfg
}
