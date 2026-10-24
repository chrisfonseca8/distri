package main

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type config struct {
	CaseName        string
	OrderURL        string
	OrchestratorURL string
	InventoryURL    string
	PaymentURL      string
	DatabaseURL     string
	ResultsDir      string
	GatewayMode     string
	GatewayLatency  time.Duration
	Warmup          time.Duration
	Duration        time.Duration
	OfferedRate     float64
	Workload        string
	HotStock        int64
	Clients         int
	Runs            int
	ExactOrders     int
	DrainTimeout    time.Duration
	SampleEvery     time.Duration
	Behavior        string
	DBPoolSize      int
	DBIdleSize      int
	ClientSteps     []int
	RateSteps       []float64
	Environment     string
	MaxConns        int
}

// closedLoopClientOverride lists the cases whose client count LOAD_CLIENTS may change.
var closedLoopClientOverride = map[string]bool{
	"C2": true, "C5": true, "C6": true, "C7": true, "C9": true, "C10": true,
	"C11": true, "C12": true, "C13": true, "C14": true,
}

// localDatabaseHosts are the only hosts this destructive runner may target without
// an explicit allow-list entry. The runner truncates tables and resets inventory.
var localDatabaseHosts = map[string]bool{"postgres": true, "localhost": true, "127.0.0.1": true, "::1": true}

// checkDatabaseTarget refuses to run against a database host that is neither local
// nor named in LOAD_TEST_ALLOWED_DB_HOST. It exists so a stray DATABASE_URL (for
// example the non-test one in a developer .env) cannot be truncated by mistake.
func checkDatabaseTarget(databaseURL, allowedHost string) error {
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.Hostname() == "" {
		return fmt.Errorf("DATABASE_URL is not a valid database URL")
	}
	host := parsed.Hostname()
	if localDatabaseHosts[host] {
		return nil
	}
	if allowedHost == "" {
		return fmt.Errorf("refusing to run: this runner truncates tables and the database host %q is not local; set LOAD_TEST_ALLOWED_DB_HOST to exactly that host to confirm it is the disposable test database", host)
	}
	if host != allowedHost {
		return fmt.Errorf("refusing to run: database host %q does not match LOAD_TEST_ALLOWED_DB_HOST %q", host, allowedHost)
	}
	return nil
}

// loadConfig reads the load test settings from the environment.
func loadConfig() (config, error) {
	c := config{
		CaseName: strings.ToUpper(env("LOAD_CASE", "C1")), OrderURL: strings.TrimRight(env("ORDER_URL", "http://127.0.0.1:8080"), "/"), OrchestratorURL: strings.TrimRight(env("ORCHESTRATOR_URL", "http://127.0.0.1:8083"), "/"),
		InventoryURL: strings.TrimRight(env("INVENTORY_URL", "http://127.0.0.1:8081"), "/"), PaymentURL: strings.TrimRight(env("PAYMENT_URL", "http://127.0.0.1:8082"), "/"),
		DatabaseURL: os.Getenv("DATABASE_URL"), ResultsDir: env("RESULTS_DIR", "test-results/load-test"),
		GatewayMode: env("GATEWAY_MODE", "success"), DrainTimeout: 15 * time.Second, SampleEvery: 500 * time.Millisecond, Workload: "uniform",
	}
	var err error
	c.GatewayLatency, err = time.ParseDuration(env("GATEWAY_LATENCY", "200ms"))
	if err != nil || c.GatewayLatency < 0 {
		return c, fmt.Errorf("GATEWAY_LATENCY must be a non-negative duration")
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if err := checkDatabaseTarget(c.DatabaseURL, strings.TrimSpace(os.Getenv("LOAD_TEST_ALLOWED_DB_HOST"))); err != nil {
		return c, err
	}
	switch c.CaseName {
	case "C1":
		c.Clients, c.Runs, c.ExactOrders = 1, 1, 20
	case "C2":
		c.Clients, c.Runs, c.Duration, c.Warmup = 10, 3, 60*time.Second, 10*time.Second
	case "C3":
		c.Runs, c.Duration, c.Warmup = 6, 60*time.Second, 10*time.Second
	case "C4":
		c.Runs, c.Duration, c.Warmup = 4, 30*time.Second, 10*time.Second
	case "C5":
		c.Clients, c.Runs, c.Warmup, c.Workload = 50, 2, 10*time.Second, "hot"
	case "C6":
		c.Clients, c.Runs, c.Duration, c.Warmup, c.Workload = 50, 1, 60*time.Second, 10*time.Second, "skewed"
	case "C7":
		c.Clients, c.Runs, c.Duration, c.Warmup, c.Workload = 50, 1, 60*time.Second, 10*time.Second, "multi-item"
	case "C9":
		c.Clients, c.Runs, c.Duration, c.Warmup, c.Behavior = 50, 1, 60*time.Second, 10*time.Second, "duplicate-checkout"
	case "C11":
		c.Clients, c.Runs, c.Duration, c.Warmup = 50, 1, 60*time.Second, 10*time.Second
	case "C12":
		c.Clients, c.Runs, c.Duration, c.Warmup = 50, 1, 60*time.Second, 10*time.Second
	case "C13":
		c.Clients, c.Runs, c.Duration, c.Warmup = 50, 1, 30*time.Second, 10*time.Second
	case "C14":
		c.Clients, c.Runs, c.Duration, c.Warmup = 50, 1, 30*time.Second, 10*time.Second
		c.DBPoolSize, err = envInt("DB_POOL_SIZE", 10)
		if err != nil {
			return c, err
		}
		// MaxIdleConns defaults to the pool size so connection churn is not a second variable.
		c.DBIdleSize, err = envInt("DB_IDLE_SIZE", c.DBPoolSize)
		if err != nil {
			return c, err
		}
	default:
		return c, fmt.Errorf("unsupported LOAD_CASE %q", c.CaseName)
	}
	if c.CaseName == "C1" {
		if c.ExactOrders, err = envInt("LOAD_ORDERS", c.ExactOrders); err != nil {
			return c, err
		}
	} else if closedLoopClientOverride[c.CaseName] {
		// LOAD_CLIENTS raises the client count of a closed-loop case, for example to
		// push it past its saturation point. Such a run is not the canonical case.
		if c.Clients, err = envInt("LOAD_CLIENTS", c.Clients); err != nil {
			return c, err
		}
	}
	if c.CaseName == "C2" {
		if c.Runs, err = envInt("LOAD_RUNS", c.Runs); err != nil {
			return c, err
		}
		if raw := os.Getenv("LOAD_DURATION"); raw != "" {
			c.Duration, err = time.ParseDuration(raw)
			if err != nil {
				return c, fmt.Errorf("LOAD_DURATION: %w", err)
			}
		}
	}
	c.Environment = strings.TrimSpace(os.Getenv("LOAD_TEST_ENVIRONMENT"))
	// Cap on simultaneous connections from the runner to the order service. Past the
	// saturation point more requests are in flight than the default cap allows, which
	// would queue them inside the client instead of loading the server.
	if c.MaxConns, err = envInt("LOAD_MAX_CONNS", 200); err != nil {
		return c, err
	}
	if c.MaxConns <= 0 {
		return c, fmt.Errorf("LOAD_MAX_CONNS must be positive")
	}
	if c.CaseName == "C3" {
		steps, err := envIntList("LOAD_CLIENT_STEPS")
		if err != nil {
			return c, err
		}
		if len(steps) > 0 {
			c.ClientSteps, c.Runs = steps, len(steps)
		}
	}
	if c.CaseName == "C4" {
		steps, err := envIntList("LOAD_RATE_STEPS")
		if err != nil {
			return c, err
		}
		if len(steps) > 0 {
			c.RateSteps = make([]float64, len(steps))
			for i, step := range steps {
				c.RateSteps[i] = float64(step)
			}
			c.Runs = len(steps)
		}
	}
	if (c.CaseName != "C3" && c.CaseName != "C4" && c.Clients <= 0) || c.Runs <= 0 || (c.CaseName == "C1" && c.ExactOrders <= 0) || (c.CaseName != "C1" && c.CaseName != "C5" && c.Duration <= 0) {
		return c, fmt.Errorf("load parameters must be positive")
	}
	return c, nil
}

// experiment returns the settings for one run of the chosen case, such as one step of a sweep.
func (c config) experiment(runNumber int) config {
	result := c
	if c.CaseName == "C3" {
		steps := []int{1, 2, 5, 10, 20, 50}
		if len(c.ClientSteps) > 0 {
			steps = c.ClientSteps
		}
		result.Clients = steps[runNumber-1]
	}
	if c.CaseName == "C4" {
		steps := []float64{10, 20, 40, 80}
		if len(c.RateSteps) > 0 {
			steps = c.RateSteps
		}
		result.OfferedRate = steps[runNumber-1]
		result.Clients = 0
	}
	if c.CaseName == "C5" {
		if runNumber == 1 {
			result.Duration, result.HotStock = 60*time.Second, fixtureStock
		} else {
			result.ExactOrders, result.HotStock = 2000, 1000
		}
	}
	return result
}

// env returns the trimmed environment variable, or fallback when it is empty.
func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

// envInt reads an integer environment variable, or returns fallback when it is unset.
func envInt(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return value, nil
}

// envIntList parses a comma-separated list of positive integers, for example
// LOAD_CLIENT_STEPS=1,5,20,100. An unset variable returns an empty list.
func envIntList(name string) ([]int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value <= 0 {
			return nil, fmt.Errorf("%s must be a comma-separated list of positive integers, got %q", name, raw)
		}
		values = append(values, value)
	}
	return values, nil
}
