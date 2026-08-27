package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/recsys/backend/internal/config"
	"github.com/recsys/backend/testsuite"
)

func main() {
	var (
		scopeFilter = flag.String("scope", "all", "Filter by scope: all, unit, integration, edge, stress, stress-light, stress-heavy, vector, redis, db, handlers, workers, api")
		pattern     = flag.String("filter", "", "Filter tests by name or description substring")
		verbose     = flag.Bool("v", false, "Enable verbose per-test execution trace logs")
		jsonOutput  = flag.Bool("json", false, "Output results in JSON format")
		noColor     = flag.Bool("no-color", false, "Disable colorized terminal formatting")
		timeout     = flag.Duration("timeout", 180*time.Second, "Test execution global timeout")
	)
	flag.Parse()

	cfg := config.Load()
	reporter := testsuite.NewReporter(os.Stdout, *noColor, *verbose)
	runner := testsuite.NewRunner(cfg, reporter)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	// Initialize infrastructure (Postgres, Redis, Qdrant, FastEmbed)
	_ = runner.InitInfra(ctx)
	defer runner.Close()

	// Register all suites
	runner.RegisterBatch(testsuite.VectorSuite())
	runner.RegisterBatch(testsuite.HandlersSuite(runner))
	runner.RegisterBatch(testsuite.WorkersSuite(runner))
	runner.RegisterBatch(testsuite.RedisSuite(runner))
	runner.RegisterBatch(testsuite.DBSuite(runner))
	runner.RegisterBatch(testsuite.APISuite(runner))
	runner.RegisterBatch(testsuite.EdgeCasesSuite(runner))
	runner.RegisterBatch(testsuite.IntegrationSuite(runner))
	runner.RegisterBatch(testsuite.StressSuite(runner))

	summary, err := runner.Run(ctx, *scopeFilter, *pattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error executing test suite: %v\n", err)
		os.Exit(1)
	}

	if *jsonOutput {
		if err := reporter.PrintJSON(summary); err != nil {
			fmt.Fprintf(os.Stderr, "Error encoding JSON summary: %v\n", err)
			os.Exit(1)
		}
	}

	if summary.Failed > 0 {
		os.Exit(1)
	}
	os.Exit(0)
}
