package main

import (
	"math"
	"testing"
)

func statsSample(usage, system, cpus, memory uint64) dockerStats {
	var s dockerStats
	s.CPUStats.CPUUsage.TotalUsage = usage
	s.CPUStats.SystemUsage = system
	s.CPUStats.OnlineCPUs = cpus
	s.MemoryStats.Usage = memory
	return s
}

func newTestSampler() *containerSampler {
	values := map[string]*containerAccumulator{}
	for name := range logicalContainers {
		values[name] = &containerAccumulator{}
	}
	return &containerSampler{values: values, ids: map[string]string{}}
}

// A container with a large lifetime CPU total but no CPU use between two samples
// must read as idle: the old one-shot formula reported lifetime averages instead.
func TestRecordComputesCPUFromConsecutiveSamples(t *testing.T) {
	s := newTestSampler()
	s.record("order", statsSample(5_000_000_000, 100_000_000_000, 12, 1000), nil)
	s.record("order", statsSample(5_000_000_000, 100_100_000_000, 12, 1000), nil)
	result := s.finish()["order"]
	if result.CPUMeanPercent == nil || *result.CPUMeanPercent != 0 {
		t.Fatalf("idle container must read 0%% CPU, got %v", result.CPUMeanPercent)
	}

	s = newTestSampler()
	s.record("order", statsSample(1_000_000_000, 100_000_000_000, 12, 1000), nil)
	// Used 0.05 s of CPU while the 12-core system advanced 1 s of CPU time: 0.05/1*12*100 = 60% of one core.
	s.record("order", statsSample(1_050_000_000, 101_000_000_000, 12, 3000), nil)
	result = s.finish()["order"]
	if result.CPUMeanPercent == nil || math.Abs(*result.CPUMeanPercent-60) > 1e-9 || *result.CPUMaxPercent != *result.CPUMeanPercent {
		t.Fatalf("expected 60%% CPU, got %v", result.CPUMeanPercent)
	}
	if *result.MemoryMeanBytes != 2000 || *result.MemoryMaxBytes != 3000 || result.Samples != 2 {
		t.Fatalf("unexpected memory summary: %#v", result)
	}
}

func TestFirstSampleOnlyEstablishesBaseline(t *testing.T) {
	s := newTestSampler()
	s.record("payment", statsSample(9_000_000_000, 100_000_000_000, 4, 500), nil)
	result := s.finish()["payment"]
	if !result.Available || result.CPUMeanPercent != nil {
		t.Fatalf("one sample must report memory but no CPU, got %#v", result)
	}
}

func TestMissingPostgresContainerIsOptionalAndExplained(t *testing.T) {
	s := newTestSampler()
	s.record("postgres", dockerStats{}, errNoContainer)
	results := s.finish()
	if results["postgres"].Available || results["postgres"].UnavailableReason != noLocalPostgres {
		t.Fatalf("expected a managed-database explanation, got %#v", results["postgres"])
	}
	for _, name := range requiredContainers {
		if name == "postgres" {
			t.Fatal("postgres must not be a required container")
		}
	}
}
