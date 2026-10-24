package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

type suite struct {
	ctx             context.Context
	db              *pgx.Conn
	client          *http.Client
	orderURL        string
	orchestratorURL string
	inventoryURL    string
	paymentURL      string
	gatewayMode     string
	runLabel        string
	composeFile     string
	resultsDir      string
	runID           string
}

type phaseContext struct {
	suite      *suite
	definition phaseDefinition
	mu         sync.Mutex
	latencies  []time.Duration
	httpErrors int
	assertions []assertionResult
	notes      []string
	skipped    bool
}

// newSuite reads the service URLs and settings from the environment and connects to the database.
func newSuite(ctx context.Context) (*suite, error) {
	databaseURL := requiredEnv("DATABASE_URL")
	db, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect correctness database: %w", err)
	}
	return &suite{
		ctx: ctx, db: db, client: &http.Client{Timeout: 12 * time.Second},
		orderURL: requiredEnv("ORDER_URL"), orchestratorURL: requiredEnv("ORCHESTRATOR_URL"), inventoryURL: requiredEnv("INVENTORY_URL"),
		paymentURL: requiredEnv("PAYMENT_URL"), gatewayMode: envOr("GATEWAY_MODE", "success"),
		runLabel:    envOr("RUN_LABEL", envOr("GATEWAY_MODE", "success")),
		composeFile: strings.TrimSpace(os.Getenv("COMPOSE_FILE")),
		resultsDir:  envOr("RESULTS_DIR", "test-results/correctness-test"),
		runID:       time.Now().UTC().Format("20060102T150405.000000000Z"),
	}, nil
}

// canControlServices reports whether COMPOSE_FILE is set, so phases can stop and start services.
func (s *suite) canControlServices() bool { return s.composeFile != "" }

// compose runs a docker compose action on one service, with a timeout.
func (s *suite) compose(action, service string) error {
	if !s.canControlServices() {
		return fmt.Errorf("COMPOSE_FILE is not set")
	}
	ctx, cancel := context.WithTimeout(s.ctx, 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "docker", "compose", "-f", s.composeFile, action, service)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose %s %s: %w: %s", action, service, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// requiredEnv returns an environment variable and panics if it is empty.
func requiredEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		panic("required environment variable is empty: " + name)
	}
	return strings.TrimRight(value, "/")
}

// envOr returns the trimmed environment variable, or fallback when it is empty.
func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

// execute runs one phase, times it and turns a panic into a failed result.
func (s *suite) execute(definition phaseDefinition) (result phaseResult) {
	pc := &phaseContext{suite: s, definition: definition}
	started := time.Now()
	fmt.Printf("\nPHASE %02d — %s\n%s\n", definition.Number, definition.Name, definition.Description)
	defer func() {
		if recovered := recover(); recovered != nil {
			pc.fail("phase completed without panic", fmt.Sprint(recovered))
		}
		status := "PASS"
		if pc.skipped {
			status = "SKIP"
		} else {
			for _, assertion := range pc.assertions {
				if !assertion.Passed {
					status = "FAIL"
					break
				}
			}
		}
		result = phaseResult{definition.Number, definition.Name, definition.Description, status,
			milliseconds(time.Since(started)), summarize(pc.latencies, pc.httpErrors), pc.assertions, pc.notes}
		fmt.Printf("%s | requests=%d errors=%d p50=%.2fms p95=%.2fms p99=%.2fms duration=%.2fms\n",
			status, result.Metrics.Requests, result.Metrics.Errors, result.Metrics.P50MS,
			result.Metrics.P95MS, result.Metrics.P99MS, result.DurationMS)
	}()
	definition.Run(pc)
	return
}

// check records one pass or fail assertion for the phase.
func (p *phaseContext) check(name string, condition bool, detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.assertions = append(p.assertions, assertionResult{name, condition, detail})
}

// fail records a failed assertion.
func (p *phaseContext) fail(name, detail string) { p.check(name, false, detail) }

// note adds a line of explanation to the phase's report.
func (p *phaseContext) note(note string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.notes = append(p.notes, note)
}

// skip marks the phase as skipped and records why.
func (p *phaseContext) skip(reason string) { p.skipped = true; p.note(reason) }
