package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

type containerSampler struct {
	client *http.Client
	every  time.Duration
	mu     sync.Mutex
	values map[string]*containerAccumulator
}

type containerAccumulator struct {
	samples     int64
	cpuTotal    float64
	cpuMax      float64
	memoryTotal uint64
	memoryMax   uint64
	err         error
}

type dockerStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs  uint64 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
}

func newContainerSampler(every time.Duration) *containerSampler {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
	}}
	values := map[string]*containerAccumulator{}
	for _, name := range []string{"order", "inventory", "payment", "postgres"} {
		values[name] = &containerAccumulator{}
	}
	return &containerSampler{client: &http.Client{Transport: transport, Timeout: 4 * time.Second}, every: every, values: values}
}

func (s *containerSampler) run(ctx context.Context) {
	s.sampleAll(ctx)
	ticker := time.NewTicker(s.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sampleAll(ctx)
		}
	}
}

func (s *containerSampler) sampleAll(ctx context.Context) {
	containers := map[string]string{
		"order": "distri-local-order-service-1", "inventory": "distri-local-inventory-service-1",
		"payment": "distri-local-payment-service-1", "postgres": "distri-local-postgres-1",
	}
	var wait sync.WaitGroup
	for logicalName, containerName := range containers {
		wait.Add(1)
		go func(name, dockerName string) {
			defer wait.Done()
			stats, err := s.read(ctx, dockerName)
			s.record(name, stats, err)
		}(logicalName, containerName)
	}
	wait.Wait()
}

func (s *containerSampler) read(ctx context.Context, name string) (dockerStats, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/"+name+"/stats?stream=false&one-shot=true", nil)
	if err != nil {
		return dockerStats{}, err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return dockerStats{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return dockerStats{}, fmt.Errorf("Docker returned HTTP %d", response.StatusCode)
	}
	var stats dockerStats
	if err := json.NewDecoder(response.Body).Decode(&stats); err != nil {
		return dockerStats{}, err
	}
	return stats, nil
}

func (s *containerSampler) record(name string, stats dockerStats, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.values[name]
	if err != nil {
		if value.err == nil {
			value.err = err
		}
		return
	}
	cpuDelta := stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage
	systemDelta := stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage
	cpu := 0.0
	if systemDelta > 0 && cpuDelta > 0 {
		cpus := stats.CPUStats.OnlineCPUs
		if cpus == 0 {
			cpus = 1
		}
		cpu = float64(cpuDelta) / float64(systemDelta) * float64(cpus) * 100
	}
	memory := stats.MemoryStats.Usage
	if cache := stats.MemoryStats.Stats["inactive_file"]; memory >= cache {
		memory -= cache
	}
	value.samples++
	value.cpuTotal += cpu
	value.memoryTotal += memory
	if cpu > value.cpuMax {
		value.cpuMax = cpu
	}
	if memory > value.memoryMax {
		value.memoryMax = memory
	}
}

func (s *containerSampler) finish() map[string]ContainerMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	results := make(map[string]ContainerMetrics, len(s.values))
	for name, value := range s.values {
		if value.samples == 0 {
			reason := "no Docker stats samples were collected"
			if value.err != nil {
				reason = value.err.Error()
			}
			results[name] = ContainerMetrics{UnavailableReason: reason}
			continue
		}
		cpuMean, cpuMax := value.cpuTotal/float64(value.samples), value.cpuMax
		memoryMean, memoryMax := value.memoryTotal/uint64(value.samples), value.memoryMax
		results[name] = ContainerMetrics{Available: true, Samples: value.samples, CPUMeanPercent: &cpuMean, CPUMaxPercent: &cpuMax, MemoryMeanBytes: &memoryMean, MemoryMaxBytes: &memoryMax}
	}
	return results
}
