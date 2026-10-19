package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// writeReports updates one aggregate JSON file and one aggregate Markdown file.
// Rerunning the same labeled scenario replaces its previous entry.
func writeReports(report correctnessReport, directory string) (string, string, error) {
	collection, err := loadCollection(directory)
	if err != nil {
		return "", "", err
	}
	replaced := false
	for index := range collection.Runs {
		if collection.Runs[index].Environment.RunLabel == report.Environment.RunLabel {
			collection.Runs[index] = report
			replaced = true
			break
		}
	}
	if !replaced {
		collection.Runs = append(collection.Runs, report)
	}
	return writeCollection(collection, directory)
}

func loadCollection(directory string) (correctnessReportCollection, error) {
	collection := correctnessReportCollection{Suite: "correctness-test"}
	raw, err := os.ReadFile(filepath.Join(directory, "correctness-test.json"))
	if os.IsNotExist(err) {
		return collection, nil
	}
	if err != nil {
		return collection, err
	}
	if err := json.Unmarshal(raw, &collection); err != nil {
		return collection, fmt.Errorf("decode existing aggregate report: %w", err)
	}
	return collection, nil
}

func writeCollection(collection correctnessReportCollection, directory string) (string, string, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", "", err
	}
	collection.Suite = "correctness-test"
	collection.UpdatedAt = time.Now().UTC()
	sort.Slice(collection.Runs, func(i, j int) bool {
		return collection.Runs[i].StartedAt.Before(collection.Runs[j].StartedAt)
	})
	jsonPath := filepath.Join(directory, "correctness-test.json")
	markdownPath := filepath.Join(directory, "correctness-test.md")
	encoded, err := json.MarshalIndent(collection, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(jsonPath, append(encoded, '\n'), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(markdownPath, []byte(markdownCollection(collection)), 0o644); err != nil {
		return "", "", err
	}
	return markdownPath, jsonPath, nil
}

// consolidateReports migrates timestamped legacy JSON reports into the one
// aggregate pair. JSON is authoritative; Markdown is regenerated from it.
func consolidateReports(source, destination string) (string, string, error) {
	paths, err := filepath.Glob(filepath.Join(source, "*.json"))
	if err != nil {
		return "", "", err
	}
	if len(paths) == 0 {
		return "", "", fmt.Errorf("no JSON reports found in %s", source)
	}
	collection := correctnessReportCollection{Suite: "correctness-test"}
	byLabel := make(map[string]correctnessReport)
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", "", err
		}
		var report correctnessReport
		if err := json.Unmarshal(raw, &report); err != nil {
			return "", "", fmt.Errorf("decode %s: %w", path, err)
		}
		label := report.Environment.RunLabel
		if label == "" {
			label = report.Environment.GatewayMode
			report.Environment.RunLabel = label
		}
		if previous, found := byLabel[label]; !found || previous.StartedAt.Before(report.StartedAt) {
			byLabel[label] = report
		}
	}
	for _, report := range byLabel {
		collection.Runs = append(collection.Runs, report)
	}
	return writeCollection(collection, destination)
}

func markdownCollection(collection correctnessReportCollection) string {
	var b strings.Builder
	b.WriteString("# Correctness test results\n\n")
	fmt.Fprintf(&b, "Updated: %s\n\n", collection.UpdatedAt.Format("2006-01-02 15:04:05Z07:00"))
	for _, report := range collection.Runs {
		fmt.Fprintf(&b, "## Run — %s\n\n", report.Environment.RunLabel)
		fmt.Fprintf(&b, "- Run ID: `%s`\n- Started: %s\n- Finished: %s\n- Gateway mode: `%s`\n",
			report.RunID, report.StartedAt.Format("2006-01-02 15:04:05Z07:00"),
			report.FinishedAt.Format("2006-01-02 15:04:05Z07:00"), report.Environment.GatewayMode)
		fmt.Fprintf(&b, "- Summary: **%d passed, %d failed, %d skipped**\n\n",
			report.Summary.Passed, report.Summary.Failed, report.Summary.Skipped)
		b.WriteString("| Phase | Test | Result | Requests | Errors | p50 | p95 | p99 |\n")
		b.WriteString("|---:|---|:---:|---:|---:|---:|---:|---:|\n")
		for _, phase := range report.Phases {
			fmt.Fprintf(&b, "| %02d | %s | %s | %d | %d | %.2f ms | %.2f ms | %.2f ms |\n",
				phase.Number, phase.Name, phase.Status, phase.Metrics.Requests, phase.Metrics.Errors,
				phase.Metrics.P50MS, phase.Metrics.P95MS, phase.Metrics.P99MS)
		}
		for _, phase := range report.Phases {
			fmt.Fprintf(&b, "\n### Phase %02d — %s: %s\n\n%s\n\n", phase.Number, phase.Name, phase.Status, phase.Description)
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
		b.WriteString("\n---\n\n")
	}
	return b.String()
}
