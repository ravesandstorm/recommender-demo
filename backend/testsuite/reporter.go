package testsuite

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Reporter formats and prints the test results and final summary.
type Reporter struct {
	w       io.Writer
	noColor bool
	verbose bool
}

func NewReporter(w io.Writer, noColor, verbose bool) *Reporter {
	return &Reporter{
		w:       w,
		noColor: noColor,
		verbose: verbose,
	}
}

// Colors & styles
const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
	colorCyan   = "\033[36m"
	colorWhite  = "\033[37m"
)

func (r *Reporter) c(color, text string) string {
	if r.noColor {
		return text
	}
	return color + text + colorReset
}

func (r *Reporter) PrintHeader(scopeFilter string, totalTests int) {
	fmt.Fprintf(r.w, "\n%s\n", r.c(colorBold+colorCyan, "══════════════════════════════════════════════════════════════════════════════════════════════════════════════"))
	fmt.Fprintf(r.w, " %s %s\n", r.c(colorBold+colorWhite, "RECOMMENDATION SYSTEM TEST SUITE & METRICS ENGINE"), r.c(colorDim, "(Go 1.26)"))
	fmt.Fprintf(r.w, " Target Scope: %s | Total Test Cases: %d | Time: %s\n",
		r.c(colorBold+colorYellow, scopeFilter),
		totalTests,
		r.c(colorDim, time.Now().Format("2006-01-02 15:04:05")),
	)
	fmt.Fprintf(r.w, "%s\n", r.c(colorBold+colorCyan, "══════════════════════════════════════════════════════════════════════════════════════════════════════════════"))
	fmt.Fprintf(r.w, " %-6s │ %-15s │ %-38s │ %-11s │ %-7s │ %-9s │ %s\n",
		"STATUS", "SCOPE", "TEST CASE", "LATENCY", "ASSERTS", "MEMORY", "EDGE CASE / DETAILS")
	fmt.Fprintf(r.w, "%s\n", r.c(colorDim, "────────┼─────────────────┼────────────────────────────────────────┼─────────────┼─────────┼───────────┼───────────────────────────────"))
}

func (r *Reporter) PrintResult(res TestResult) {
	var statusBadge string
	switch res.Status {
	case StatusPass:
		statusBadge = r.c(colorGreen+colorBold, "✓ PASS")
	case StatusFail:
		statusBadge = r.c(colorRed+colorBold, "✗ FAIL")
	case StatusSkip:
		statusBadge = r.c(colorYellow+colorBold, "- SKIP")
	}

	scopeBadge := r.c(colorPurple, fmt.Sprintf("%-15s", res.Scope))

	// Truncate or pad test name
	testName := res.Name
	if len(testName) > 38 {
		testName = testName[:35] + "..."
	}
	testNameStr := fmt.Sprintf("%-38s", testName)

	// Format latency with color threshold
	var latencyStr string
	if res.Status == StatusSkip {
		latencyStr = r.c(colorDim, "     -     ")
	} else if res.Metrics.DurationMs < 1.0 {
		latencyStr = r.c(colorGreen, fmt.Sprintf("%8.2f ms", res.Metrics.DurationMs))
	} else if res.Metrics.DurationMs < 20.0 {
		latencyStr = r.c(colorYellow, fmt.Sprintf("%8.2f ms", res.Metrics.DurationMs))
	} else {
		latencyStr = r.c(colorRed, fmt.Sprintf("%8.2f ms", res.Metrics.DurationMs))
	}

	assertsStr := fmt.Sprintf("%7d", res.Metrics.Assertions)
	memStr := fmt.Sprintf("%9s", formatBytes(res.Metrics.AllocBytes))

	var details string
	if res.EdgeCase != "" {
		details = r.c(colorCyan, fmt.Sprintf("[%s]", res.EdgeCase))
	} else if res.Description != "" {
		details = r.c(colorDim, res.Description)
	}

	if res.Status == StatusFail && res.Error != "" {
		details = r.c(colorRed+colorBold, fmt.Sprintf("ERR: %s", res.Error))
	} else if res.Status == StatusSkip && res.Error != "" {
		details = r.c(colorYellow, fmt.Sprintf("SKIP: %s", res.Error))
	}

	fmt.Fprintf(r.w, " %s │ %s │ %s │ %s │ %s │ %s │ %s\n",
		statusBadge,
		scopeBadge,
		testNameStr,
		latencyStr,
		assertsStr,
		memStr,
		details,
	)

	if r.verbose && len(res.Logs) > 0 {
		for _, logLine := range res.Logs {
			fmt.Fprintf(r.w, "        │ %s %s\n", r.c(colorDim, "↳"), r.c(colorDim, logLine))
		}
	}
}

func (r *Reporter) PrintSummary(summary *SuiteSummary) {
	fmt.Fprintf(r.w, "\n%s\n", r.c(colorBold+colorCyan, "══════════════════════════════════════════════════════════════════════════════════════════════════════════════"))
	fmt.Fprintf(r.w, " %s\n", r.c(colorBold+colorWhite, "EXECUTIVE TEST & METRICS SUMMARY"))
	fmt.Fprintf(r.w, "%s\n\n", r.c(colorBold+colorCyan, "══════════════════════════════════════════════════════════════════════════════════════════════════════════════"))

	// 1. Overview Banner
	statusColor := colorGreen
	statusWord := "ALL TESTS PASSED"
	if summary.Failed > 0 {
		statusColor = colorRed
		statusWord = fmt.Sprintf("%d TEST(S) FAILED", summary.Failed)
	}

	bannerDashCount := 102 - len(statusWord)
	if bannerDashCount < 2 {
		bannerDashCount = 2
	}
	fmt.Fprintf(r.w, "  ┌─ %s %s┐\n", r.c(colorBold+statusColor, statusWord), strings.Repeat("─", bannerDashCount))
	fmt.Fprintf(r.w, "  │  Total Tests:  %-6d │ Passed:      %s │ Failed:      %s │ Skipped:     %s │\n",
		summary.TotalTests,
		r.c(colorGreen+colorBold, fmt.Sprintf("%-6d", summary.Passed)),
		r.c(colorRed+colorBold, fmt.Sprintf("%-6d", summary.Failed)),
		r.c(colorYellow+colorBold, fmt.Sprintf("%-6d", summary.Skipped)),
	)
	fmt.Fprintf(r.w, "  │  Pass Rate:    %-6s │ Total Time:  %-10s │ Total Allocs:%-10d │ Total Mem:   %-8s │\n",
		r.c(colorBold+colorWhite, fmt.Sprintf("%5.1f%%", summary.PassRate)),
		summary.TotalDuration.Round(time.Millisecond).String(),
		summary.TotalAllocs,
		formatBytes(summary.TotalBytes),
	)
	fmt.Fprintf(r.w, "  └──────────────────────────────────────────────────────────────────────────────────────────────────────┘\n\n")

	// 2. Scope Breakdown Table
	fmt.Fprintf(r.w, " %s\n", r.c(colorBold+colorYellow, "▸ FUNCTIONAL SCOPE & PERFORMANCE BREAKDOWN"))
	fmt.Fprintf(r.w, " %-16s │ %7s │ %7s │ %7s │ %11s │ %11s │ %11s │ %9s\n",
		"SCOPE", "TOTAL", "PASSED", "FAILED", "AVG LATENCY", "MAX LATENCY", "ALLOCS", "MEMORY")
	fmt.Fprintf(r.w, " %s\n", r.c(colorDim, "─────────────────┼─────────┼─────────┼─────────┼─────────────┼─────────────┼─────────────┼───────────"))

	// Sort scopes alphabetically
	scopes := make([]Scope, 0, len(summary.ScopeStats))
	for s := range summary.ScopeStats {
		scopes = append(scopes, s)
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i] < scopes[j] })

	for _, s := range scopes {
		st := summary.ScopeStats[s]
		scopeStr := r.c(colorPurple, fmt.Sprintf("%-16s", string(s)))
		totalStr := fmt.Sprintf("%7d", st.Total)
		passStr := r.c(colorGreen, fmt.Sprintf("%7d", st.Passed))
		var failStr string
		if st.Failed > 0 {
			failStr = r.c(colorRed+colorBold, fmt.Sprintf("%7d", st.Failed))
		} else {
			failStr = r.c(colorDim, fmt.Sprintf("%7d", st.Failed))
		}
		avgLatStr := fmt.Sprintf("%8.2f ms", st.AvgDurationMs)
		maxLatStr := fmt.Sprintf("%8.2f ms", st.MaxDurationMs)
		allocsStr := fmt.Sprintf("%11d", st.TotalAllocs)
		memStr := fmt.Sprintf("%9s", formatBytes(st.TotalBytes))

		fmt.Fprintf(r.w, " %s │ %s │ %s │ %s │ %s │ %s │ %s │ %s\n",
			scopeStr,
			totalStr,
			passStr,
			failStr,
			avgLatStr,
			maxLatStr,
			allocsStr,
			memStr,
		)
	}

	// 3. Edge Case Matrix Table
	if len(summary.EdgeStats) > 0 {
		fmt.Fprintf(r.w, "\n %s\n", r.c(colorBold+colorYellow, "▸ EDGE CASE & BOUNDARY CONDITION COVERAGE MATRIX"))
		fmt.Fprintf(r.w, " %-38s │ %-9s │ %7s │ %7s │ %s\n",
			"EDGE CASE / BOUNDARY CONDITION", "STATUS", "PASSED", "FAILED", "COVERAGE VERDICT")
		fmt.Fprintf(r.w, " %s\n", r.c(colorDim, "───────────────────────────────────────┼───────────┼─────────┼─────────┼──────────────────────────────────"))

		edgeTags := make([]EdgeCaseTag, 0, len(summary.EdgeStats))
		for t := range summary.EdgeStats {
			edgeTags = append(edgeTags, t)
		}
		sort.Slice(edgeTags, func(i, j int) bool { return edgeTags[i] < edgeTags[j] })

		for _, tag := range edgeTags {
			es := summary.EdgeStats[tag]
			tagStr := fmt.Sprintf("%-38s", string(tag))
			var statusBadge string
			var verdict string
			if es.Failed > 0 {
				statusBadge = r.c(colorRed+colorBold, fmt.Sprintf("%-9s", "FAILED"))
				verdict = r.c(colorRed, fmt.Sprintf("Broken: %d failed cases", es.Failed))
			} else if es.Passed > 0 {
				statusBadge = r.c(colorGreen+colorBold, fmt.Sprintf("%-9s", "COVERED"))
				verdict = r.c(colorGreen, "Protected & Verified")
			} else {
				statusBadge = r.c(colorYellow, fmt.Sprintf("%-9s", "SKIPPED"))
				verdict = r.c(colorYellow, "Skipped due to offline infra")
			}
			passStr := fmt.Sprintf("%7d", es.Passed)
			failStr := fmt.Sprintf("%7d", es.Failed)

			fmt.Fprintf(r.w, " %s │ %s │ %s │ %s │ %s\n",
				tagStr,
				statusBadge,
				passStr,
				failStr,
				verdict,
			)
		}
	}

	fmt.Fprintf(r.w, "\n%s\n\n", r.c(colorBold+colorCyan, "══════════════════════════════════════════════════════════════════════════════════════════════════════════════"))
}

func (r *Reporter) PrintJSON(summary *SuiteSummary) error {
	enc := json.NewEncoder(r.w)
	enc.SetIndent("", "  ")
	return enc.Encode(summary)
}

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
