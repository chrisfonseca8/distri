package jobs

import (
	"testing"
	"time"
)

func TestRetryBackoffExponentialGrowthAndCap(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: 2 * time.Second},
		{attempt: 2, want: 4 * time.Second},
		{attempt: 3, want: 8 * time.Second},
		{attempt: 4, want: 16 * time.Second},
		{attempt: 5, want: 32 * time.Second},
		{attempt: 6, want: 60 * time.Second},
		{attempt: 7, want: 60 * time.Second},
	}

	for _, test := range tests {
		got := RetryBackoff(test.attempt, 2*time.Second, time.Minute, 0.25, 0.5)
		if got != test.want {
			t.Errorf("attempt %d delay = %s, want %s", test.attempt, got, test.want)
		}
	}
}

func TestRetryBackoffJitter(t *testing.T) {
	tests := []struct {
		name   string
		sample float64
		want   time.Duration
	}{
		{name: "negative 25 percent", sample: 0, want: 6 * time.Second},
		{name: "no jitter", sample: 0.5, want: 8 * time.Second},
		{name: "positive 25 percent", sample: 1, want: 10 * time.Second},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := RetryBackoff(3, 2*time.Second, time.Minute, 0.25, test.sample)
			if got != test.want {
				t.Errorf("delay = %s, want %s", got, test.want)
			}
		})
	}
}

func TestRetryBackoffStrictFinalCap(t *testing.T) {
	got := RetryBackoff(6, 2*time.Second, time.Minute, 0.25, 1)
	if got != time.Minute {
		t.Fatalf("delay = %s, want strict cap of 1m", got)
	}
}
