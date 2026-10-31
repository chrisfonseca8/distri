package config

import (
	"testing"
	"time"
)

func TestLoadSagaConfigDefaults(t *testing.T) {
	isolateSagaConfigTest(t)
	t.Setenv("SAGA_DEADLINE", "")
	t.Setenv("SAGA_STEP_TIMEOUT", "")

	config, err := LoadSagaConfig()
	if err != nil {
		t.Fatalf("load saga config: %v", err)
	}
	if config.Deadline != 15*time.Minute || config.StepTimeout != 30*time.Second {
		t.Fatalf("unexpected defaults: %+v", config)
	}
}

func TestSagaConfigRejectsInvalidDurations(t *testing.T) {
	isolateSagaConfigTest(t)
	t.Setenv("SAGA_DEADLINE", "1m")
	t.Setenv("SAGA_STEP_TIMEOUT", "1m")

	if _, err := LoadSagaConfig(); err == nil {
		t.Fatal("LoadSagaConfig accepted a step timeout equal to the deadline")
	}
}

func isolateSagaConfigTest(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, name := range []string{
		"SAGA_DEADLINE",
		"SAGA_STEP_TIMEOUT",
		"SAGA_SWEEP_INTERVAL",
		"SAGA_SWEEP_BATCH",
		"SAGA_STEP_MAX_ATTEMPTS",
		"SAGA_RESEND_BACKOFF_CAP",
		"SAGA_STUCK_REFUND_AFTER",
		"SAGA_UNDO_REVIVE_AFTER",
		"SAGA_UNDO_MAX_REVIVES",
	} {
		t.Setenv(name, "")
	}
}
