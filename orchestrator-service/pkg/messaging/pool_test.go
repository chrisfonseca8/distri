package messaging

import (
	"testing"
	"time"
)

func TestDesiredWorkersCanReachThirtyAndCapsThere(t *testing.T) {
	config := PoolConfig{
		Min:            1,
		Max:            30,
		DepthPerWorker: 20,
		Interval:       5 * time.Second,
		ScaleDownAfter: 30 * time.Second,
	}

	for _, test := range []struct {
		depth int
		want  int
	}{
		{depth: 0, want: 1},
		{depth: 1, want: 2},
		{depth: 93, want: 6},
		{depth: 561, want: 30},
		{depth: 10_000, want: 30},
	} {
		if got := desiredWorkers(test.depth, config); got != test.want {
			t.Fatalf("desiredWorkers(%d) = %d, want %d", test.depth, got, test.want)
		}
	}
}
