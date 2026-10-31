package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadWorkerConfigDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
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
	if workerConfig.Concurrency != 4 ||
		workerConfig.Lease != 30*time.Second ||
		workerConfig.HandlerTimeout != 20*time.Second ||
		workerConfig.PollInterval != 200*time.Millisecond ||
		workerConfig.NotReadyDelay != time.Second ||
		workerConfig.RetryBase != 2*time.Second ||
		workerConfig.RetryCap != time.Minute ||
		workerConfig.RetryJitter != 0.25 {
		t.Fatalf("unexpected worker defaults: %+v", workerConfig)
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
