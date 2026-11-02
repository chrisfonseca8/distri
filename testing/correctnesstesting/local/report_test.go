package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConsolidateReportsAcceptsCollections(t *testing.T) {
	source := t.TempDir()
	destination := filepath.Join(t.TempDir(), "combined")
	now := time.Now().UTC()
	for index, mode := range []string{"success", "decline", "timeout"} {
		report := correctnessReport{
			Suite: "correctness-test", StartedAt: now.Add(time.Duration(index) * time.Second),
			Environment: reportEnv{GatewayMode: mode, RunLabel: mode},
			Summary:     reportSummary{Total: 1, Passed: 1},
		}
		encoded, err := json.Marshal(correctnessReportCollection{Suite: "correctness-test", Runs: []correctnessReport{report}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, mode+".json"), encoded, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, jsonPath, err := consolidateReports(source, destination)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var combined correctnessReportCollection
	if err := json.Unmarshal(raw, &combined); err != nil {
		t.Fatal(err)
	}
	if len(combined.Runs) != 3 {
		t.Fatalf("runs = %d, want 3", len(combined.Runs))
	}
}
