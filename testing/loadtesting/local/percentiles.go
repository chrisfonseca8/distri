package main

import (
	"sort"
	"time"
)

// SummarizeLatencies uses nearest-rank observed samples, matching correctness reports.
func SummarizeLatencies(samples []time.Duration) LatencySummary {
	summary := LatencySummary{Samples: int64(len(samples))}
	if len(samples) == 0 {
		return summary
	}
	values := append([]time.Duration(nil), samples...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	var total time.Duration
	for _, value := range values {
		total += value
	}
	summary.MinMS = durationMS(values[0])
	summary.MeanMS = durationMS(total / time.Duration(len(values)))
	summary.P50MS = durationMS(nearest(values, 0.50))
	summary.P95MS = durationMS(nearest(values, 0.95))
	summary.P99MS = durationMS(nearest(values, 0.99))
	summary.MaxMS = durationMS(values[len(values)-1])
	return summary
}

// nearest returns the value at the given percentile of sorted values.
func nearest(values []time.Duration, percentile float64) time.Duration {
	index := int(float64(len(values)-1)*percentile + 0.5)
	return values[index]
}

// durationMS converts a duration to fractional milliseconds.
func durationMS(value time.Duration) float64 {
	return float64(value.Microseconds()) / 1000
}
