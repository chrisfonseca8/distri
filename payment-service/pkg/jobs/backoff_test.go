package jobs

import (
	"testing"
	"time"
)

func TestRetryBackoffGrowthJitterAndStrictCap(t *testing.T) {
	wants := []time.Duration{2, 4, 8, 16, 32, 60, 60}
	for index, seconds := range wants {
		got := RetryBackoff(index+1, 2*time.Second, time.Minute, 0.25, 0.5)
		want := seconds * time.Second
		if got != want {
			t.Errorf("attempt %d delay = %s, want %s", index+1, got, want)
		}
	}
	if got := RetryBackoff(3, 2*time.Second, time.Minute, 0.25, 0); got != 6*time.Second {
		t.Errorf("negative jitter delay = %s, want 6s", got)
	}
	if got := RetryBackoff(3, 2*time.Second, time.Minute, 0.25, 1); got != 10*time.Second {
		t.Errorf("positive jitter delay = %s, want 10s", got)
	}
	if got := RetryBackoff(6, 2*time.Second, time.Minute, 0.25, 1); got != time.Minute {
		t.Errorf("capped jitter delay = %s, want 1m", got)
	}
}
