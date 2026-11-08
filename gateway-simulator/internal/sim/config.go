package sim

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the simulator's behaviour. Every timing here is an ASSUMPTION, not a
// measurement of Razorpay.
type Config struct {
	Addr      string
	KeyID     string
	KeySecret string

	// Seed makes every random decision reproducible: a decision is a function of
	// (Seed, stable identifier), never of a shared random sequence.
	Seed uint64
	// TimeScale divides every scheduled delay (10 = ten times faster). A run with
	// TimeScale != 1 is accelerated test time, not a wall-clock profile.
	TimeScale float64

	// AutoCapture is the account-level capture setting.
	AutoCapture bool
	// ScenarioMix assigns a payment scenario per checkout, e.g. "capture=80,decline=15,none=5".
	ScenarioMix []WeightedScenario

	// APILatency is the HTTP response latency profile: zero, normal or slow.
	APILatency string
	// PayDelayMedian is the time from order creation to the payment attempt.
	PayDelayMedian time.Duration
	// CaptureDelayMedian is the time from authorization to automatic capture.
	CaptureDelayMedian time.Duration
	// LookupDelayMin/Max hide a new order from list-by-receipt for a seeded time
	// in [min,max]. The real API was observed to lag ~17 s (one sample).
	LookupDelayMin time.Duration
	LookupDelayMax time.Duration
	// RefundDelayMedian is how long a refund stays pending.
	RefundDelayMedian time.Duration

	// EnforceReceiptUnique reproduces the DOCUMENTED behaviour (a repeated receipt
	// is rejected). The default (false) reproduces what the real API was observed
	// to do: accept the duplicate and make a second order.
	EnforceReceiptUnique bool
	// DuplicatePays is a SIMULATOR POLICY, not a Razorpay guarantee: which orders
	// of a duplicated receipt the simulated customer pays. "first" (default) pays
	// only the first order; "all" applies the checkout's scenario to every one.
	// Duplicate receipts being accepted is observed Razorpay behaviour; whether a
	// customer would pay one of them is not, so a scenario can pin any order.
	DuplicatePays string

	// EnforceRefundReceiptUnique makes a repeated refund receipt on one payment a
	// rejection (the documented error text). The default (false) accepts duplicates:
	// whether the real API prevents a duplicate refund through the receipt is not
	// established, so the simulator is not made more convenient than that.
	EnforceRefundReceiptUnique bool
	// RefundKeyRetention is how long an X-Refund-Idempotency key is remembered;
	// zero means forever. Razorpay documents no retention, so any finite value is an
	// injected fault ("the key was forgotten").
	RefundKeyRetention time.Duration

	// Webhook delivery. An empty WebhookURL disables it.
	WebhookURL       string
	WebhookSecret    string // dedicated simulation webhook secret (not the API key secret)
	WebhookAccountID string
	WebhookWorkers   int
	// WebhookTimeout is the acknowledgement timeout. Documented: Razorpay waits 5 s.
	WebhookTimeout time.Duration
	// WebhookSchedule is the delay before each retry; the last entry repeats until
	// WebhookMaxAge. Razorpay documents only "exponential backoff for 24 hours", so
	// this schedule is OURS (assumed). The accelerated profile exists for short
	// functional checks and is not a faithful wall-clock profile.
	WebhookSchedule []time.Duration
	WebhookMaxAge   time.Duration
	// WebhookDelayMedian is the time from a state transition to the first delivery
	// attempt (assumed).
	WebhookDelayMedian time.Duration
}

type WeightedScenario struct {
	Name   string
	Weight int
}

// LoadConfig reads the SIM_* environment variables into a validated Config.
func LoadConfig() (Config, error) {
	cfg := Config{
		Addr:               envString("SIM_ADDR", ":9090"),
		KeyID:              envString("SIM_KEY_ID", "rzp_test_simulator"),
		KeySecret:          envString("SIM_KEY_SECRET", "simulator_secret"),
		TimeScale:          1,
		AutoCapture:        true,
		APILatency:         envString("SIM_API_LATENCY", "zero"),
		PayDelayMedian:     2 * time.Second,
		CaptureDelayMedian: 500 * time.Millisecond,
		RefundDelayMedian:  time.Second,
	}
	var err error
	seed, err := envInt("SIM_SEED", 1)
	if err != nil {
		return Config{}, err
	}
	cfg.Seed = uint64(seed)
	if cfg.TimeScale, err = envFloat("SIM_TIME_SCALE", 1); err != nil {
		return Config{}, err
	}
	if cfg.AutoCapture, err = envBool("SIM_AUTO_CAPTURE", true); err != nil {
		return Config{}, err
	}
	if cfg.EnforceReceiptUnique, err = envBool("SIM_ENFORCE_RECEIPT_UNIQUE", false); err != nil {
		return Config{}, err
	}
	for name, target := range map[string]*time.Duration{
		"SIM_PAY_DELAY":        &cfg.PayDelayMedian,
		"SIM_CAPTURE_DELAY":    &cfg.CaptureDelayMedian,
		"SIM_LOOKUP_DELAY_MIN": &cfg.LookupDelayMin,
		"SIM_LOOKUP_DELAY_MAX": &cfg.LookupDelayMax,
		"SIM_REFUND_DELAY":     &cfg.RefundDelayMedian,
	} {
		if value := os.Getenv(name); value != "" {
			parsed, err := time.ParseDuration(value)
			if err != nil {
				return Config{}, fmt.Errorf("%s must be a duration: %w", name, err)
			}
			*target = parsed
		}
	}
	cfg.DuplicatePays = envString("SIM_DUPLICATE_PAYS", "first")
	if cfg.EnforceRefundReceiptUnique, err = envBool("SIM_ENFORCE_REFUND_RECEIPT_UNIQUE", false); err != nil {
		return Config{}, err
	}
	if value := os.Getenv("SIM_REFUND_KEY_RETENTION"); value != "" {
		if cfg.RefundKeyRetention, err = time.ParseDuration(value); err != nil {
			return Config{}, fmt.Errorf("SIM_REFUND_KEY_RETENTION must be a duration: %w", err)
		}
	}
	cfg.WebhookURL = strings.TrimSpace(os.Getenv("SIM_WEBHOOK_URL"))
	cfg.WebhookSecret = os.Getenv("SIM_WEBHOOK_SECRET")
	cfg.WebhookAccountID = envString("SIM_WEBHOOK_ACCOUNT_ID", "acc_simulator")
	if cfg.WebhookWorkers, err = envInt("SIM_WEBHOOK_WORKERS", 4); err != nil {
		return Config{}, err
	}
	cfg.WebhookTimeout = 5 * time.Second
	cfg.WebhookMaxAge = 24 * time.Hour
	cfg.WebhookDelayMedian = 300 * time.Millisecond
	for name, target := range map[string]*time.Duration{
		"SIM_WEBHOOK_TIMEOUT": &cfg.WebhookTimeout,
		"SIM_WEBHOOK_MAX_AGE": &cfg.WebhookMaxAge,
		"SIM_WEBHOOK_DELAY":   &cfg.WebhookDelayMedian,
	} {
		if value := os.Getenv(name); value != "" {
			parsed, err := time.ParseDuration(value)
			if err != nil {
				return Config{}, fmt.Errorf("%s must be a duration: %w", name, err)
			}
			*target = parsed
		}
	}
	schedule := "5s,25s,2m,10m,1h"
	if envString("SIM_WEBHOOK_PROFILE", "standard") == "accelerated" {
		schedule = "1s,2s,4s,8s"
	}
	if cfg.WebhookSchedule, err = parseSchedule(envString("SIM_WEBHOOK_SCHEDULE", schedule)); err != nil {
		return Config{}, err
	}
	mix, err := ParseMix(envString("SIM_SCENARIO_MIX", "capture=100"))
	if err != nil {
		return Config{}, err
	}
	cfg.ScenarioMix = mix
	return cfg, cfg.Validate()
}

// Validate rejects missing keys and out-of-range or unknown settings.
func (c Config) Validate() error {
	if c.KeyID == "" || c.KeySecret == "" {
		return errors.New("SIM_KEY_ID and SIM_KEY_SECRET are required")
	}
	if c.TimeScale <= 0 {
		return errors.New("SIM_TIME_SCALE must be greater than zero")
	}
	switch c.APILatency {
	case "zero", "normal", "slow":
	default:
		return fmt.Errorf("SIM_API_LATENCY must be zero, normal or slow, got %q", c.APILatency)
	}
	if c.DuplicatePays != "first" && c.DuplicatePays != "all" {
		return fmt.Errorf("SIM_DUPLICATE_PAYS must be first or all, got %q", c.DuplicatePays)
	}
	if c.WebhookURL != "" {
		if c.WebhookSecret == "" {
			return errors.New("SIM_WEBHOOK_SECRET is required when SIM_WEBHOOK_URL is set")
		}
		if c.WebhookWorkers <= 0 || c.WebhookTimeout <= 0 || c.WebhookMaxAge <= 0 || len(c.WebhookSchedule) == 0 {
			return errors.New("webhook workers, timeout, max age and schedule must be positive")
		}
	}
	if c.LookupDelayMax < c.LookupDelayMin {
		return errors.New("SIM_LOOKUP_DELAY_MAX must not be below SIM_LOOKUP_DELAY_MIN")
	}
	for _, d := range []time.Duration{c.PayDelayMedian, c.CaptureDelayMedian, c.RefundDelayMedian, c.LookupDelayMin} {
		if d < 0 {
			return errors.New("simulator delays must not be negative")
		}
	}
	return nil
}

// scaled applies TimeScale to a delay.
func (c Config) scaled(d time.Duration) time.Duration {
	return time.Duration(float64(d) / c.TimeScale)
}

// ParseMix parses a comma-separated list of name=weight pairs into a weighted scenario list.
func ParseMix(spec string) ([]WeightedScenario, error) {
	var mix []WeightedScenario
	total := 0
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, weight, found := strings.Cut(part, "=")
		if !found {
			return nil, fmt.Errorf("scenario mix entry %q must look like name=weight", part)
		}
		if !ValidScenario(name) {
			return nil, fmt.Errorf("unknown scenario %q", name)
		}
		w, err := strconv.Atoi(weight)
		if err != nil || w < 0 {
			return nil, fmt.Errorf("scenario weight for %q must be a non-negative integer", name)
		}
		mix = append(mix, WeightedScenario{Name: name, Weight: w})
		total += w
	}
	if total == 0 {
		return nil, errors.New("scenario mix needs a positive total weight")
	}
	return mix, nil
}

// parseSchedule parses a comma-separated list of durations.
func parseSchedule(spec string) ([]time.Duration, error) {
	var schedule []time.Duration
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		d, err := time.ParseDuration(part)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("SIM_WEBHOOK_SCHEDULE entry %q must be a positive duration", part)
		}
		schedule = append(schedule, d)
	}
	if len(schedule) == 0 {
		return nil, errors.New("SIM_WEBHOOK_SCHEDULE needs at least one delay")
	}
	return schedule, nil
}

// envString returns the trimmed environment variable, or fallback when it is empty.
func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

// envInt reads an integer environment variable, or returns fallback when it is unset.
func envInt(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return parsed, nil
}

// envFloat reads a float environment variable, or returns fallback when it is unset.
func envFloat(name string, fallback float64) (float64, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number: %w", name, err)
	}
	return parsed, nil
}

// envBool reads a boolean environment variable, or returns fallback when it is unset.
func envBool(name string, fallback bool) (bool, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", name, err)
	}
	return parsed, nil
}
