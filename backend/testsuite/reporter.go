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
	bgGreen     = "\033[42m\033[30m"
	bgRed       = "\033[41m\033[37m"
	bgYellow    = "\033[43m\033[30m"
	bgBlue      = "\033[44m\033[37m"
	bgPurple    = "\033[45m\033[37m"
)

func (r *Reporter) c(color, text string) string {
	if r.noColor {
		return text
	}
	return color + text + colorReset
}

func (r *Reporter) PrintHeader(scopeFilter string, totalTests int) {
	fmt.Fprintf(r.w, "\n%s\n", r.c(colorBold+colorCyan, "═════════════════════════════════════════════════════════════════════════════════════════════════"))
	fmt.Fprintf(r.w, " %s %s\n", r.c(colorBold+colorWhite, "RECOMMENDATION SYSTEM TEST SUITE & METRICS ENGINE"), r.c(colorDim, "(Go 1.26)"))
	fmt.Fprintf(r.w, " Target Scope: %s | Total Test Cases: %d | Time: %s\n",
		r.c(colorBold+colorYellow, scopeFilter),
		totalTests,
		r.c(colorDim, time.Now().Format("2006-01-02 15:04:05")),
	)
	fmt.Fprintf(r.w, "%s\n", r.c(colorBold+colorCyan, "═════════════════════════════════════════════════════════════════════════════════════════════════"))
	fmt.Fprintf(r.w, "%-7s │ %-15s │ %-38s │ %-9s │ %-7s │ %-8s │ %s\n",
		"STATUS", "SCOPE", "TEST CASE", "LATENCY", "ASSERTS", "MEMORY", "EDGE CASE / DETAILS")
	fmt.Fprintf(r.w, "%s\n", r.c(colorDim, "────────┼─────────────────┼────────────────────────────────────────┼───────────┼─────────┼──────────┼───────────────────────────────"))
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

	// Format latency with color threshold
	var latencyStr string
	if res.Status == StatusSkip {
		latencyStr = r.c(colorDim, "   -   ")
	} else if res.Metrics.DurationMs < 1.0 {
		latencyStr = r.c(colorGreen, fmt.Sprintf("%6.2f ms", res.Metrics.DurationMs))
	} else if res.Metrics.DurationMs < 20.0 {
		latencyStr = r.c(colorYellow, fmt.Sprintf("%6.2f ms", res.Metrics.DurationMs))
	} else {
		latencyStr = r.c(colorRed, fmt.Sprintf("%6.2f ms", res.Metrics.DurationMs))
	}

	assertsStr := fmt.Sprintf("%5d", res.Metrics.Assertions)

	// Format memory allocated
	var memStr string
	if res.Metrics.AllocBytes < 1024 {
		memStr = fmt.Sprintf("%4d B ", res.Metrics.AllocBytes)
	} else if res.Metrics.AllocBytes < 1024*1024 {
		memStr = fmt.Sprintf("%4.1f KB", float64(res.Metrics.AllocBytes)/1024.0)
	} else {
		memStr = fmt.Sprintf("%4.1f MB", float64(res.Metrics.AllocBytes)/(1024.0*1024.0))
	}

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

	fmt.Fprintf(r.w, "%-7s │ %s │ %-38s │ %s │ %s │ %8s │ %s\n",
		statusBadge,
		scopeBadge,
		testName,
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
	fmt.Fprintf(r.w, "\n%s\n", r.c(colorBold+colorCyan, "═════════════════════════════════════════════════════════════════════════════════════════════════"))
	fmt.Fprintf(r.w, " %s\n", r.c(colorBold+colorWhite, "EXECUTIVE TEST & METRICS SUMMARY"))
	fmt.Fprintf(r.w, "%s\n\n", r.c(colorBold+colorCyan, "═════════════════════════════════════════════════════════════════════════════════════════════════"))

	// 1. Overview Banner
	statusColor := colorGreen
	statusWord := "ALL TESTS PASSED"
	if summary.Failed > 0 {
		statusColor = colorRed
		statusWord = fmt.Sprintf("%d TEST(S) FAILED", summary.Failed)
	}

	fmt.Fprintf(r.w, "  ┌─ %s ────────────────────────────────────────────────────────────────────────────┐\n", r.c(colorBold+statusColor, statusWord))
	fmt.Fprintf(r.w, "  │  Total Tests:    %-6d │ Passed:      %s │ Failed:      %s │ Skipped:     %s │\n",
		summary.TotalTests,
		r.c(colorGreen+colorBold, fmt.Sprintf("%-6d", summary.Passed)),
		r.c(colorRed+colorBold, fmt.Sprintf("%-6d", summary.Failed)),
		r.c(colorYellow+colorBold, fmt.Sprintf("%-6d", summary.Skipped)),
	)
	fmt.Fprintf(r.w, "  │  Pass Rate:      %s │ Total Time:  %-8s │ Total Allocs:%-8d │ Total Mem:   %-8s │\n",
		r.c(colorBold+colorWhite, fmt.Sprintf("%5.1f%%", summary.PassRate)),
		summary.TotalDuration.Round(time.Millisecond).String(),
		summary.TotalAllocs,
		formatBytes(summary.TotalBytes),
	)
	fmt.Fprintf(r.w, "  └────────────────────────────────────────────────────────────────────────────────────────┘\n\n")

	// 2. Scope Breakdown Table
	fmt.Fprintf(r.w, " %s\n", r.c(colorBold+colorYellow, "▸ FUNCTIONAL SCOPE & PERFORMANCE BREAKDOWN"))
	fmt.Fprintf(r.w, " %-16s │ %-5s │ %-6s │ %-6s │ %-11s │ %-11s │ %-11s │ %s\n",
		"SCOPE", "TOTAL", "PASSED", "FAILED", "AVG LATENCY", "MAX LATENCY", "ALLOCS", "MEMORY")
	fmt.Fprintf(r.w, " %s\n", r.c(colorDim, "─────────────────┼───────┼────────┼────────┼─────────────┼─────────────┼─────────────┼──────────"))

	// Sort scopes alphabetically
	scopes := make([]Scope, 0, len(summary.ScopeStats))
	for s := range summary.ScopeStats {
		scopes = append(scopes, s)
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i] < scopes[j] })

	for _, s := range scopes {
		st := summary.ScopeStats[s]
		passBadge := r.c(colorGreen, fmt.Sprintf("%d", st.Passed))
		failBadge := fmt.Sprintf("%d", st.Failed)
		if st.Failed > 0 {
			failBadge = r.c(colorRed+colorBold, failBadge)
		} else {
			failBadge = r.c(colorDim, failBadge)
		}

		fmt.Fprintf(r.w, " %-16s │ %5d │ %6s │ %6s │ %8.2f ms │ %8.2f ms │ %11d │ %s\n",
			r.c(colorPurple, string(s)),
			st.Total,
			passBadge,
			failBadge,
			st.AvgDurationMs,
			st.MaxDurationMs,
			st.TotalAllocs,
			formatBytes(st.TotalBytes),
		)
	}

	// 3. Edge Case Matrix Table
	if len(summary.EdgeStats) > 0 {
		fmt.Fprintf(r.w, "\n %s\n", r.c(colorBold+colorYellow, "▸ EDGE CASE & BOUNDARY CONDITION COVERAGE MATRIX"))
		fmt.Fprintf(r.w, " %-38s │ %-7s │ %-6s │ %-6s │ %s\n",
			"EDGE CASE / BOUNDARY CONDITION", "STATUS", "PASS", "FAIL", "COVERAGE VERDICT")
		fmt.Fprintf(r.w, " %s\n", r.c(colorDim, "───────────────────────────────────────┼─────────┼────────┼────────┼──────────────────────────────────"))

		edgeTags := make([]EdgeCaseTag, 0, len(summary.EdgeStats))
		for t := range summary.EdgeStats {
			edgeTags = append(edgeTags, t)
		}
		sort.Slice(edgeTags, func(i, j int) bool { return edgeTags[i] < edgeTags[j] })

		for _, tag := range edgeTags {
			es := summary.EdgeStats[tag]
			var statusBadge string
			var verdict string
			if es.Failed > 0 {
				statusBadge = r.c(colorRed+colorBold, "FAIL")
				verdict = r.c(colorRed, fmt.Sprintf("Broken: %d failed cases", es.Failed))
			} else if es.Passed > 0 {
				statusBadge = r.c(colorGreen+colorBold, "COVERED")
				verdict = r.c(colorGreen, "Protected & Verified")
			} else {
				statusBadge = r.c(colorYellow, "SKIPPED")
				verdict = r.c(colorYellow, "Skipped due to offline infra")
			}

			fmt.Fprintf(r.w, " %-38s │ %-7s │ %6d │ %6d │ %s\n",
				tag,
				statusBadge,
				es.Passed,
				es.Failed,
				verdict,
			)
		}
	}

	fmt.Fprintf(r.w, "\n%s\n\n", r.c(colorBold+colorCyan, "═════════════════════════════════════════════════════════════════════════════════════════════════"))
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

// Diagnostics checks and prints live service readiness.
func PrintDiagnostics(w io.Writer, noColor bool, pgURL, redisAddr, qdrantURL, embedURL string) {
	fmt.Fprintf(w, "  ┌─ System Environment Health Check ──────────────────────────────────────┐\n")
	fmt.Fprintf(w, "  │ Postgres:   %-57s │\n", pgURL)
	fmt.Fprintf(w, "  │ Redis:      %-57s │\n", redisAddr)
	fmt.Fprintf(w, "  │ Qdrant:     %-57s │\n", qdrantURL)
	fmt.Fprintf(w, "  │ FastEmbed:  %-57s │\n", embedURL)
	fmt.Fprintf(w, "  └────────────────────────────────────────────────────────────────────────┘\n\n")
}

func (r *Reporter) HorizontalLine() {
	fmt.Fprintf(r.w, "%s\n", strings.Repeat("─", 97))
}
