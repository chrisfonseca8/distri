package config

import (
	"testing"
	"time"
)

func TestLoadSweeperConfigDefaults(t *testing.T) {
	for _, name := range []string{
		"RESERVATION_SWEEP_TTL",
		"RESERVATION_SWEEP_INTERVAL",
		"RESERVATION_SWEEP_BATCH_SIZE",
	} {
		t.Setenv(name, "")
	}
	config, err := LoadSweeperConfig()
	if err != nil {
		t.Fatalf("load sweeper config: %v", err)
	}
	if config.ReservationTTL != 10*time.Minute || config.Interval != time.Minute || config.BatchSize != 100 {
		t.Fatalf("unexpected defaults: %+v", config)
	}
}
