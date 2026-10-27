package config

import "testing"

import "time"

func TestLoadOutboxConcurrency(t *testing.T) {
	t.Setenv("OUTBOX_CONCURRENCY", "")
	if got, err := LoadOutboxConcurrency(); err != nil || got != 4 {
		t.Fatalf("default concurrency = %d, %v; want 4", got, err)
	}
	t.Setenv("OUTBOX_CONCURRENCY", "7")
	if got, err := LoadOutboxConcurrency(); err != nil || got != 7 {
		t.Fatalf("configured concurrency = %d, %v; want 7", got, err)
	}
	t.Setenv("OUTBOX_CONCURRENCY", "0")
	if _, err := LoadOutboxConcurrency(); err == nil {
		t.Fatal("zero concurrency must fail")
	}
}

func TestLoadOutboxPollInterval(t *testing.T) {
	t.Setenv("OUTBOX_POLL_INTERVAL", "")
	if got, err := LoadOutboxPollInterval(); err != nil || got != 100*time.Millisecond {
		t.Fatalf("default poll interval = %v, %v; want 100ms", got, err)
	}
	t.Setenv("OUTBOX_POLL_INTERVAL", "250ms")
	if got, err := LoadOutboxPollInterval(); err != nil || got != 250*time.Millisecond {
		t.Fatalf("configured poll interval = %v, %v; want 250ms", got, err)
	}
	t.Setenv("OUTBOX_POLL_INTERVAL", "0s")
	if _, err := LoadOutboxPollInterval(); err == nil {
		t.Fatal("zero poll interval must fail")
	}
}
