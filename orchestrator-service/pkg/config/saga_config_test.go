package config

import (
	"testing"
	"time"
)

func TestLoadSagaConfigDefaults(t *testing.T) {
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
	t.Setenv("SAGA_DEADLINE", "1m")
	t.Setenv("SAGA_STEP_TIMEOUT", "1m")

	if _, err := LoadSagaConfig(); err == nil {
		t.Fatal("LoadSagaConfig accepted a step timeout equal to the deadline")
	}
}
