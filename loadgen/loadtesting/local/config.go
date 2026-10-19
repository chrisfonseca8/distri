package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type config struct {
	CaseName       string
	OrderURL       string
	InventoryURL   string
	PaymentURL     string
	DatabaseURL    string
	ResultsDir     string
	GatewayMode    string
	GatewayLatency time.Duration
	Warmup         time.Duration
	Duration       time.Duration
	OfferedRate    float64
	Workload       string
	HotStock       int64
	Clients        int
	Runs           int
	ExactOrders    int
	DrainTimeout   time.Duration
	SampleEvery    time.Duration
	Behavior       string
	DBPoolSize     int
	DBIdleSize     int
}

func loadConfig() (config, error) {
	c := config{
		CaseName: strings.ToUpper(env("LOAD_CASE", "C1")), OrderURL: strings.TrimRight(env("ORDER_URL", "http://127.0.0.1:8080"), "/"),
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
	case "C10":
		c.Clients, c.Runs, c.Duration, c.Warmup, c.Behavior = 50, 1, 60*time.Second, 10*time.Second, "duplicate-pay"
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
	} else if c.CaseName == "C2" {
		if c.Clients, err = envInt("LOAD_CLIENTS", c.Clients); err != nil {
			return c, err
		}
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
	if (c.CaseName != "C4" && c.Clients <= 0) || c.Runs <= 0 || (c.CaseName == "C1" && c.ExactOrders <= 0) || (c.CaseName != "C1" && c.CaseName != "C5" && c.Duration <= 0) {
		return c, fmt.Errorf("load parameters must be positive")
	}
	return c, nil
}

func (c config) experiment(runNumber int) config {
	result := c
	if c.CaseName == "C3" {
		result.Clients = []int{1, 2, 5, 10, 20, 50}[runNumber-1]
	}
	if c.CaseName == "C4" {
		result.OfferedRate = []float64{10, 20, 40, 80}[runNumber-1]
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

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
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
