package main

import (
	"sort"
	"time"
)

// summarize turns request latencies and an error count into a latency summary.
func summarize(samples []time.Duration, errors int) metricSummary {
	m := metricSummary{Requests: len(samples), Errors: errors}
	if len(samples) == 0 {
		return m
	}
	values := append([]time.Duration(nil), samples...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	var total time.Duration
	for _, value := range values {
		total += value
	}
	m.MinMS = milliseconds(values[0])
	m.MeanMS = milliseconds(total / time.Duration(len(values)))
	m.P50MS = milliseconds(percentile(values, 0.50))
	m.P95MS = milliseconds(percentile(values, 0.95))
	m.P99MS = milliseconds(percentile(values, 0.99))
	m.MaxMS = milliseconds(values[len(values)-1])
	return m
}

// percentile returns the value at percentile p of sorted values.
func percentile(values []time.Duration, p float64) time.Duration {
	index := int(float64(len(values)-1)*p + 0.5)
	return values[index]
}

// milliseconds converts a duration to fractional milliseconds.
func milliseconds(value time.Duration) float64 {
	return float64(value.Microseconds()) / 1000
}
