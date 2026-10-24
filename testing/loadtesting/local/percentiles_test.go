package main

import (
	"testing"
	"time"
)

func TestSummarizeLatencies(t *testing.T) {
	samples := []time.Duration{100 * time.Millisecond, 10 * time.Millisecond, 50 * time.Millisecond, 20 * time.Millisecond}
	got := SummarizeLatencies(samples)
	if got.Samples != 4 || got.MinMS != 10 || got.P50MS != 50 || got.P95MS != 100 || got.P99MS != 100 || got.MaxMS != 100 {
		t.Fatalf("unexpected latency summary: %+v", got)
	}
}
