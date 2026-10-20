package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadWorkerConfigDefaults(t *testing.T) {
	for _, name := range []string{
		"WORKER_CONCURRENCY",
		"JOB_LEASE",
		"JOB_HANDLER_TIMEOUT",
		"JOB_POLL_INTERVAL",
		"JOB_NOT_READY_DELAY",
		"JOB_RETRY_BASE",
		"JOB_RETRY_CAP",
		"JOB_RETRY_JITTER",
	} {
		t.Setenv(name, "")
	}

	workerConfig, err := LoadWorkerConfig()
	if err != nil {
		t.Fatalf("load worker config: %v", err)
	}
	if workerConfig.Concurrency != 4 {
		t.Errorf("concurrency = %d, want 4", workerConfig.Concurrency)
	}
	if workerConfig.Lease != 30*time.Second {
		t.Errorf("lease = %s, want 30s", workerConfig.Lease)
	}
	if workerConfig.HandlerTimeout != 20*time.Second {
		t.Errorf("handler timeout = %s, want 20s", workerConfig.HandlerTimeout)
	}
	if workerConfig.PollInterval != 200*time.Millisecond {
		t.Errorf("poll interval = %s, want 200ms", workerConfig.PollInterval)
	}
	if workerConfig.NotReadyDelay != time.Second {
		t.Errorf("not-ready delay = %s, want 1s", workerConfig.NotReadyDelay)
	}
	if workerConfig.RetryBase != 2*time.Second {
		t.Errorf("retry base = %s, want 2s", workerConfig.RetryBase)
	}
	if workerConfig.RetryCap != 60*time.Second {
		t.Errorf("retry cap = %s, want 60s", workerConfig.RetryCap)
	}
	if workerConfig.RetryJitter != 0.25 {
		t.Errorf("retry jitter = %v, want 0.25", workerConfig.RetryJitter)
	}
}

func TestWorkerConfigRejectsHandlerTimeoutAtLeastLease(t *testing.T) {
	workerConfig := WorkerConfig{
		Concurrency:    1,
		Lease:          20 * time.Second,
		HandlerTimeout: 20 * time.Second,
		PollInterval:   time.Second,
		NotReadyDelay:  time.Second,
		RetryBase:      time.Second,
		RetryCap:       time.Minute,
		RetryJitter:    0.25,
	}

	err := workerConfig.Validate()
	if err == nil || !strings.Contains(err.Error(), "JOB_HANDLER_TIMEOUT must be less than JOB_LEASE") {
		t.Fatalf("Validate() error = %v", err)
	}
}
