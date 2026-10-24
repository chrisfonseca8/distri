package config

import "testing"

func TestRazorpayWithoutWebhookSecretRefusesToStartUnlessDisabledByName(t *testing.T) {
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
	t.Setenv("GATEWAY_PROVIDER", "simulator")
	t.Setenv("RAZORPAY_WEBHOOK_SECRET", "")
	t.Setenv("WEBHOOKS_DISABLED", "")
	if _, err := LoadWebhookConfig(); err != nil {
		t.Fatalf("the built-in simulator has no webhooks: %v", err)
	}
}
