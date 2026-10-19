package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

type suite struct {
	ctx          context.Context
	db           *pgx.Conn
	client       *http.Client
	orderURL     string
	inventoryURL string
	paymentURL   string
	gatewayMode  string
	resultsDir   string
	runID        string
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

func newSuite(ctx context.Context) (*suite, error) {
	databaseURL := requiredEnv("DATABASE_URL")
	db, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect correctness database: %w", err)
	}
	return &suite{
		ctx: ctx, db: db, client: &http.Client{Timeout: 12 * time.Second},
		orderURL: requiredEnv("ORDER_URL"), inventoryURL: requiredEnv("INVENTORY_URL"),
		paymentURL: requiredEnv("PAYMENT_URL"), gatewayMode: envOr("GATEWAY_MODE", "success"),
		resultsDir: envOr("RESULTS_DIR", "test-results/correctness-local"),
		runID:      time.Now().UTC().Format("20060102T150405.000000000Z"),
	}, nil
}

func requiredEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		panic("required environment variable is empty: " + name)
	}
	return strings.TrimRight(value, "/")
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

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

func (p *phaseContext) check(name string, condition bool, detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.assertions = append(p.assertions, assertionResult{name, condition, detail})
}
func (p *phaseContext) fail(name, detail string) { p.check(name, false, detail) }
func (p *phaseContext) note(note string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.notes = append(p.notes, note)
}
func (p *phaseContext) skip(reason string) { p.skipped = true; p.note(reason) }
