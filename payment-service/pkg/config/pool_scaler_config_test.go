package config

import "testing"

var poolScalerEnv = []string{
	"PAYMENT_DB_POOL_AUTOSCALE", "PAYMENT_DB_POOL_MAX", "PAYMENT_DB_POOL_STEP", "PAYMENT_DB_POOL_INTERVAL",
	"PAYMENT_DB_POOL_UP_WAIT", "PAYMENT_DB_POOL_UP_MIN_WAITS", "PAYMENT_DB_POOL_UP_NEAR_FULL",
	"PAYMENT_DB_POOL_UP_SAMPLES", "PAYMENT_DB_POOL_UP_COOLDOWN", "PAYMENT_DB_POOL_DOWN_USAGE",
	"PAYMENT_DB_POOL_DOWN_SAMPLES", "PAYMENT_DB_POOL_DOWN_COOLDOWN", "PAYMENT_DB_CONNECTION_BUDGET",
	"PAYMENT_DB_CONNECTION_BUDGET_RESERVED", "PAYMENT_REPLICAS", "PAYMENT_DB_POOL_GUARD_LATENCY_FACTOR",
	"PAYMENT_DB_POOL_GUARD_LATENCY_MAX", "PAYMENT_DB_POOL_GUARD_LOCK_WAITERS", "PAYMENT_DB_POOL_GUARD_CONN_ERRORS",
}

func isolatePoolScalerEnv(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, name := range poolScalerEnv {
		t.Setenv(name, "")
	}
}

func TestPoolAutoscalingIsOffByDefaultAndIgnoresOtherSettingsWhenOff(t *testing.T) {
	isolatePoolScalerEnv(t)
	c, err := LoadPoolScalerConfig()
	if err != nil || c.Enabled {
		t.Fatalf("default: enabled=%v err=%v, want off", c.Enabled, err)
	}
	t.Setenv("PAYMENT_DB_POOL_MAX", "9999") // nonsense, but unused while off
	if _, err := LoadPoolScalerConfig(); err != nil {
		t.Fatalf("an unused setting blocked a normal start: %v", err)
	}
}

func TestPoolAutoscalingNeedsAConnectionBudget(t *testing.T) {
	isolatePoolScalerEnv(t)
	t.Setenv("PAYMENT_DB_POOL_AUTOSCALE", "true")
	if _, err := LoadPoolScalerConfig(); err == nil {
		t.Fatal("autoscaling without PAYMENT_DB_CONNECTION_BUDGET was accepted")
	}
	t.Setenv("PAYMENT_DB_CONNECTION_BUDGET", "200")
	c, err := LoadPoolScalerConfig()
	if err != nil || !c.Enabled {
		t.Fatalf("with a budget: enabled=%v err=%v", c.Enabled, err)
	}
	if c.Max != 50 || c.Step != 5 || c.Interval.Seconds() != 5 || c.UpSamples != 2 || c.Replicas != 1 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if c.Ceiling() != 50 {
		t.Fatalf("ceiling %d, want 50 (hard limit) with a budget of 200 for one replica", c.Ceiling())
	}
}

func TestPoolCeilingFollowsTheBudgetAcrossReplicas(t *testing.T) {
	isolatePoolScalerEnv(t)
	t.Setenv("PAYMENT_DB_POOL_AUTOSCALE", "true")
	t.Setenv("PAYMENT_DB_CONNECTION_BUDGET", "100")
	t.Setenv("PAYMENT_DB_CONNECTION_BUDGET_RESERVED", "20")
	t.Setenv("PAYMENT_REPLICAS", "4")
	c, err := LoadPoolScalerConfig()
	if err != nil || c.Ceiling() != 20 {
		t.Fatalf("ceiling %d err=%v, want (100-20)/4 = 20", c.Ceiling(), err)
	}
}

func TestPoolAutoscalingRejectsInvalidSettings(t *testing.T) {
	bad := map[string]string{
		"PAYMENT_DB_POOL_MAX":                   "51", // above the hard limit of 50
		"PAYMENT_DB_POOL_STEP":                  "0",
		"PAYMENT_DB_POOL_INTERVAL":              "0s",
		"PAYMENT_DB_POOL_UP_WAIT":               "0s",
		"PAYMENT_DB_POOL_UP_NEAR_FULL":          "1.5",
		"PAYMENT_DB_POOL_UP_SAMPLES":            "0",
		"PAYMENT_DB_POOL_DOWN_USAGE":            "0.9", // not below the scale-up fullness
		"PAYMENT_DB_POOL_DOWN_COOLDOWN":         "-5s",
		"PAYMENT_DB_CONNECTION_BUDGET_RESERVED": "200", // not below the budget
		"PAYMENT_REPLICAS":                      "0",
		"PAYMENT_DB_POOL_GUARD_LATENCY_FACTOR":  "1",
		"PAYMENT_DB_POOL_GUARD_LOCK_WAITERS":    "0",
		"PAYMENT_DB_POOL_UP_COOLDOWN":           "soon",
	}
	for name, value := range bad {
		isolatePoolScalerEnv(t)
		t.Setenv("PAYMENT_DB_POOL_AUTOSCALE", "true")
		t.Setenv("PAYMENT_DB_CONNECTION_BUDGET", "100")
		t.Setenv(name, value)
		if _, err := LoadPoolScalerConfig(); err == nil {
			t.Fatalf("%s=%s was accepted", name, value)
		}
	}
	isolatePoolScalerEnv(t)
	t.Setenv("PAYMENT_DB_POOL_AUTOSCALE", "yes please")
	if _, err := LoadPoolScalerConfig(); err == nil {
		t.Fatal("a malformed on/off switch was accepted")
	}
}
