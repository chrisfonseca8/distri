// local-correctness executes destructive black-box and database correctness checks.
// Always point it at a freshly-created disposable local Compose database.
package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

func main() {
	ctx := context.Background()
	s, err := newSuite(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer s.db.Close(ctx)

	definitions := []phaseDefinition{
		phase01(), phase02(), phase03(), phase04(), phase05(), phase06(), phase07(),
		phase08(), phase09(), phase10(), phase11(), phase12(), phase13(), phase14(),
		phase15(), phase16(), phase17(), phase18(), phase19(), phase20(), phase21(), phase22(),
	}
	started := time.Now()
	report := correctnessReport{Suite: "correctness-local", RunID: s.runID, StartedAt: started,
		Environment: reportEnv{s.orderURL, s.inventoryURL, s.paymentURL, s.gatewayMode}}
	for _, definition := range definitions {
		result := s.execute(definition)
		report.Phases = append(report.Phases, result)
		report.Summary.Total++
		switch result.Status {
		case "PASS":
			report.Summary.Passed++
		case "FAIL":
			report.Summary.Failed++
		case "SKIP":
			report.Summary.Skipped++
		}
	}
	report.FinishedAt = time.Now()
	markdownPath, jsonPath, err := writeReports(report, s.resultsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "write correctness report:", err)
		os.Exit(2)
	}
	fmt.Printf("\nReports:\n  %s\n  %s\n", markdownPath, jsonPath)
	if report.Summary.Failed > 0 {
		os.Exit(1)
	}
}
