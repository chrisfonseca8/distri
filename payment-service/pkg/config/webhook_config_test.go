package config

import "testing"

func TestRazorpayWithoutWebhookSecretRefusesToStartUnlessDisabledByName(t *testing.T) {
	isolateWebhookConfigTest(t)
	t.Setenv("GATEWAY_PROVIDER", "razorpay")
	t.Setenv("RAZORPAY_WEBHOOK_SECRET", "")

	t.Setenv("WEBHOOKS_DISABLED", "")
	if _, err := LoadWebhookConfig(); err == nil {
		t.Fatal("a missing webhook secret with the razorpay provider must be refused")
	}

	t.Setenv("WEBHOOKS_DISABLED", "true")
	config, err := LoadWebhookConfig()
	if err != nil || config.Enabled() {
		t.Fatalf("explicit opt-out: err=%v enabled=%v, want no error and webhooks off", err, config.Enabled())
	}

	t.Setenv("WEBHOOKS_DISABLED", "")
	t.Setenv("RAZORPAY_WEBHOOK_SECRET", "a-secret")
	if config, err := LoadWebhookConfig(); err != nil || !config.Enabled() {
		t.Fatalf("with a secret: err=%v enabled=%v, want enabled", err, config.Enabled())
	}
}

func TestOtherProvidersNeverNeedAWebhookSecret(t *testing.T) {
	isolateWebhookConfigTest(t)
	t.Setenv("GATEWAY_PROVIDER", "simulator")
	t.Setenv("RAZORPAY_WEBHOOK_SECRET", "")
	t.Setenv("WEBHOOKS_DISABLED", "")
	if _, err := LoadWebhookConfig(); err != nil {
		t.Fatalf("the built-in simulator has no webhooks: %v", err)
	}
}

func isolateWebhookConfigTest(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, name := range []string{
		"GATEWAY_PROVIDER",
		"RAZORPAY_WEBHOOK_SECRET",
		"WEBHOOKS_DISABLED",
		"WEBHOOK_MAX_BODY_BYTES",
		"WEBHOOK_PROCESS_INTERVAL",
		"WEBHOOK_PROCESS_BATCH",
		"PAYMENT_WEBHOOK_WORKERS",
		"PAYMENT_WEBHOOK_AUTOSCALE",
		"PAYMENT_WEBHOOK_WORKERS_MIN",
		"PAYMENT_WEBHOOK_WORKERS_MAX",
		"PAYMENT_WEBHOOK_SCALE_INTERVAL",
		"PAYMENT_WEBHOOK_SCALE_UP_AGE",
		"PAYMENT_WEBHOOK_SCALE_UP_MIN_BACKLOG",
		"PAYMENT_WEBHOOK_SCALE_UP_SAMPLES",
		"PAYMENT_WEBHOOK_SCALE_UP_COOLDOWN",
		"PAYMENT_WEBHOOK_SCALE_DOWN_AGE",
		"PAYMENT_WEBHOOK_SCALE_DOWN_BACKLOG",
		"PAYMENT_WEBHOOK_SCALE_DOWN_SAMPLES",
		"PAYMENT_WEBHOOK_SCALE_DOWN_COOLDOWN",
		"PAYMENT_WEBHOOK_SCALE_DOWN_UTILIZATION",
		"PAYMENT_WEBHOOK_POOL_RESERVE",
		"WEBHOOK_LEASE",
		"WEBHOOK_MAX_ATTEMPTS",
		"WEBHOOK_BACKOFF_BASE",
		"WEBHOOK_BACKOFF_CAP",
	} {
		t.Setenv(name, "")
	}
}

func TestWebhookWorkersDefaultToOneAndAcceptAPositiveInteger(t *testing.T) {
	isolateWebhookConfigTest(t)
	config, err := LoadWebhookConfig()
	if err != nil || config.Workers != 1 {
		t.Fatalf("default: workers=%d err=%v, want 1 and no error", config.Workers, err)
	}
	t.Setenv("PAYMENT_WEBHOOK_WORKERS", "2")
	config, err = LoadWebhookConfig()
	if err != nil || config.Workers != 2 {
		t.Fatalf("PAYMENT_WEBHOOK_WORKERS=2: workers=%d err=%v, want 2", config.Workers, err)
	}
	// The other webhook settings are untouched by the worker count.
	if config.Batch != defaultWebhookBatch || config.Interval != defaultWebhookInterval || config.Lease != defaultWebhookLease {
		t.Fatalf("worker count changed batch/interval/lease: %+v", config)
	}
}

func TestWebhookWorkersRejectZeroNegativeAndNonIntegers(t *testing.T) {
	for _, value := range []string{"0", "-1", "abc", "1.5", "2 "} {
		isolateWebhookConfigTest(t)
		t.Setenv("PAYMENT_WEBHOOK_WORKERS", value)
		if _, err := LoadWebhookConfig(); err == nil {
			t.Fatalf("PAYMENT_WEBHOOK_WORKERS=%q was accepted, want an error", value)
		}
	}
}

func TestWebhookAutoscaleIsOffByDefaultAndAMalformedScaleSettingDoesNotBlockAFixedStart(t *testing.T) {
	isolateWebhookConfigTest(t)
	config, err := LoadWebhookConfig()
	if err != nil || config.Autoscale {
		t.Fatalf("default: autoscale=%v err=%v, want off", config.Autoscale, err)
	}
	if config.Scale.Min != 1 || config.Scale.Max != 10 || config.Workers != 1 {
		t.Fatalf("defaults: min=%d max=%d start=%d, want 1, 10, 1", config.Scale.Min, config.Scale.Max, config.Workers)
	}
	t.Setenv("PAYMENT_WEBHOOK_SCALE_DOWN_UTILIZATION", "7") // invalid, but autoscaling is off
	if _, err := LoadWebhookConfig(); err != nil {
		t.Fatalf("an unused scaling setting blocked a fixed-size start: %v", err)
	}
}

func TestWebhookAutoscaleAcceptsAValidConfigurationAndRejectsInvalidOnes(t *testing.T) {
	isolateWebhookConfigTest(t)
	t.Setenv("PAYMENT_WEBHOOK_AUTOSCALE", "true")
	config, err := LoadWebhookConfig()
	if err != nil || !config.Autoscale {
		t.Fatalf("defaults with autoscale on: autoscale=%v err=%v", config.Autoscale, err)
	}
	if config.Scale.Interval.Seconds() != 5 || config.Scale.UpSamples != 2 || config.Scale.DownUtilization != 0.6 {
		t.Fatalf("unexpected defaults: %+v", config.Scale)
	}
	bad := map[string]string{
		"PAYMENT_WEBHOOK_WORKERS_MAX":            "0",
		"PAYMENT_WEBHOOK_WORKERS_MIN":            "0",
		"PAYMENT_WEBHOOK_SCALE_INTERVAL":         "0s",
		"PAYMENT_WEBHOOK_SCALE_UP_AGE":           "100ms", // not above the scale-down age
		"PAYMENT_WEBHOOK_SCALE_UP_MIN_BACKLOG":   "3",     // not above the scale-down backlog
		"PAYMENT_WEBHOOK_SCALE_UP_SAMPLES":       "0",
		"PAYMENT_WEBHOOK_SCALE_DOWN_UTILIZATION": "1",
		"PAYMENT_WEBHOOK_SCALE_UP_COOLDOWN":      "-1s",
		"PAYMENT_WEBHOOK_POOL_RESERVE":           "-1",
		"PAYMENT_WEBHOOK_SCALE_DOWN_SAMPLES":     "abc",
		"PAYMENT_WEBHOOK_WORKERS":                "11", // starting count above the cap of 10
	}
	for name, value := range bad {
		isolateWebhookConfigTest(t)
		t.Setenv("PAYMENT_WEBHOOK_AUTOSCALE", "true")
		t.Setenv(name, value)
		if _, err := LoadWebhookConfig(); err == nil {
			t.Fatalf("%s=%s was accepted with autoscaling on", name, value)
		}
	}
	isolateWebhookConfigTest(t)
	t.Setenv("PAYMENT_WEBHOOK_AUTOSCALE", "maybe")
	if _, err := LoadWebhookConfig(); err == nil {
		t.Fatal("PAYMENT_WEBHOOK_AUTOSCALE=maybe was accepted")
	}
}
