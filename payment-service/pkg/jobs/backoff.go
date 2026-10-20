package jobs

import "time"

// RetryBackoff returns an exponential retry delay with symmetric jitter.
// The final result is clamped so cap remains a strict maximum after jitter.
func RetryBackoff(
	attempt int,
	base time.Duration,
	cap time.Duration,
	jitter float64,
	randomSample float64,
) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	delay := base
	for currentAttempt := 1; currentAttempt < attempt && delay < cap; currentAttempt++ {
		if delay > cap/2 {
			delay = cap
			break
		}
		delay *= 2
	}
	if delay > cap {
		delay = cap
	}

	if randomSample < 0 {
		randomSample = 0
	} else if randomSample > 1 {
		randomSample = 1
	}

	factor := 1 - jitter + (2 * jitter * randomSample)
	delay = time.Duration(float64(delay) * factor)
	if delay > cap {
		return cap
	}
	return delay
}
