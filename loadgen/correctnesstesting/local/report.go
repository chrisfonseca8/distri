package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func writeReports(report correctnessReport, directory string) (string, string, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", "", err
	}
	base := "correctness-local-" + report.RunID
	jsonPath := filepath.Join(directory, base+".json")
	markdownPath := filepath.Join(directory, base+".md")
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(jsonPath, append(encoded, '\n'), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(markdownPath, []byte(markdownReport(report)), 0o644); err != nil {
		return "", "", err
	}
	return markdownPath, jsonPath, nil
}

func markdownReport(report correctnessReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Local correctness test result\n\n")
	fmt.Fprintf(&b, "- Run: `%s`\n- Started: %s\n- Finished: %s\n- Gateway mode: `%s`\n",
		report.RunID, report.StartedAt.Format("2006-01-02 15:04:05Z07:00"),
		report.FinishedAt.Format("2006-01-02 15:04:05Z07:00"), report.Environment.GatewayMode)
	fmt.Fprintf(&b, "- Summary: **%d passed, %d failed, %d skipped**\n\n",
		report.Summary.Passed, report.Summary.Failed, report.Summary.Skipped)
	fmt.Fprintf(&b, "| Phase | Test | Result | Requests | Errors | p50 | p95 | p99 |\n")
	fmt.Fprintf(&b, "|---:|---|:---:|---:|---:|---:|---:|---:|\n")
	for _, phase := range report.Phases {
		fmt.Fprintf(&b, "| %02d | %s | %s | %d | %d | %.2f ms | %.2f ms | %.2f ms |\n",
			phase.Number, phase.Name, phase.Status, phase.Metrics.Requests, phase.Metrics.Errors,
			phase.Metrics.P50MS, phase.Metrics.P95MS, phase.Metrics.P99MS)
	}
	for _, phase := range report.Phases {
		fmt.Fprintf(&b, "\n## Phase %02d — %s: %s\n\n%s\n\n", phase.Number, phase.Name, phase.Status, phase.Description)
		fmt.Fprintf(&b, "Duration %.2f ms. HTTP latency: min %.2f, mean %.2f, p50 %.2f, p95 %.2f, p99 %.2f, max %.2f ms (%d requests, %d transport/decode errors).\n",
			phase.DurationMS, phase.Metrics.MinMS, phase.Metrics.MeanMS, phase.Metrics.P50MS,
			phase.Metrics.P95MS, phase.Metrics.P99MS, phase.Metrics.MaxMS, phase.Metrics.Requests, phase.Metrics.Errors)
		if len(phase.Assertions) > 0 {
			b.WriteString("\nAssertions:\n\n")
			for _, assertion := range phase.Assertions {
				mark := "PASS"
				if !assertion.Passed {
					mark = "FAIL"
				}
				fmt.Fprintf(&b, "- **%s** — %s", mark, assertion.Name)
				if assertion.Detail != "" {
					fmt.Fprintf(&b, ": %s", assertion.Detail)
				}
				b.WriteByte('\n')
			}
		}
		if len(phase.Notes) > 0 {
			b.WriteString("\nNotes:\n\n")
			for _, note := range phase.Notes {
				fmt.Fprintf(&b, "- %s\n", note)
			}
		}
	}
	return b.String()
}
