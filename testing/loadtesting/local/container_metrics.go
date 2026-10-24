package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// logicalContainers maps the report names to Docker Compose service names.
// Containers are found by the Compose service label, so the project name
// (distri-local locally, distri in the cloud) does not matter.
var logicalContainers = map[string]string{
	"order": "order-service", "inventory": "inventory-service",
	"payment": "payment-service", "postgres": "postgres",
}

// requiredContainers must be sampled for a run to be valid. PostgreSQL is
// optional: with a managed database (Neon) there is no local container.
var requiredContainers = []string{"order", "inventory", "payment"}

const noLocalPostgres = "no local PostgreSQL container (managed database); CPU and memory are not sampled"

type containerSampler struct {
	client *http.Client
	every  time.Duration
	mu     sync.Mutex
	ids    map[string]string
	values map[string]*containerAccumulator
}

type containerAccumulator struct {
	samples     int64
	cpuSamples  int64
	cpuTotal    float64
	cpuMax      float64
	memoryTotal uint64
	memoryMax   uint64
	prevUsage   uint64
	prevSystem  uint64
	havePrev    bool
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
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
}

// newContainerSampler creates a sampler that reads container CPU and memory through the Docker socket.
func newContainerSampler(every time.Duration) *containerSampler {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
	}}
	values := map[string]*containerAccumulator{}
	for name := range logicalContainers {
		values[name] = &containerAccumulator{}
	}
	return &containerSampler{
		client: &http.Client{Transport: transport, Timeout: 4 * time.Second},
		every:  every, ids: map[string]string{}, values: values,
	}
}

// run samples every container on each tick until ctx is cancelled.
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

// sampleAll samples all containers at the same time.
func (s *containerSampler) sampleAll(ctx context.Context) {
	var wait sync.WaitGroup
	for logicalName, serviceName := range logicalContainers {
		wait.Add(1)
		go func(name, service string) {
			defer wait.Done()
			stats, err := s.readService(ctx, name, service)
			s.record(name, stats, err)
		}(logicalName, serviceName)
	}
	wait.Wait()
}

// readService resolves the container id once and reads one stats sample. A 404
// clears the cached id so a recreated container is found on the next sample.
func (s *containerSampler) readService(ctx context.Context, logicalName, service string) (dockerStats, error) {
	s.mu.Lock()
	id := s.ids[logicalName]
	s.mu.Unlock()
	if id == "" {
		found, err := s.findContainer(ctx, service)
		if err != nil {
			return dockerStats{}, err
		}
		id = found
		s.mu.Lock()
		s.ids[logicalName] = id
		s.mu.Unlock()
	}
	stats, status, err := s.readStats(ctx, id)
	if status == http.StatusNotFound {
		s.mu.Lock()
		delete(s.ids, logicalName)
		s.mu.Unlock()
	}
	return stats, err
}

var errNoContainer = errors.New("container not found")

// findContainer returns the ID of the running container for a compose service.
func (s *containerSampler) findContainer(ctx context.Context, service string) (string, error) {
	filters, err := json.Marshal(map[string][]string{"label": {
		"com.docker.compose.service=" + service, "com.docker.compose.oneoff=False",
	}})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/json?filters="+url.QueryEscape(string(filters)), nil)
	if err != nil {
		return "", err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Docker returned HTTP %d listing containers", response.StatusCode)
	}
	var containers []struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&containers); err != nil {
		return "", err
	}
	if len(containers) == 0 {
		return "", fmt.Errorf("%w: compose service %q", errNoContainer, service)
	}
	return containers[0].ID, nil
}

// readStats uses one-shot stats. In one-shot mode Docker does not fill the
// previous-sample counters, so CPU must be computed from two of our own samples.
func (s *containerSampler) readStats(ctx context.Context, id string) (dockerStats, int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/"+id+"/stats?stream=false&one-shot=true", nil)
	if err != nil {
		return dockerStats{}, 0, err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return dockerStats{}, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return dockerStats{}, response.StatusCode, fmt.Errorf("Docker returned HTTP %d", response.StatusCode)
	}
	var stats dockerStats
	if err := json.NewDecoder(response.Body).Decode(&stats); err != nil {
		return dockerStats{}, response.StatusCode, err
	}
	return stats, response.StatusCode, nil
}

// record adds one sample. CPU percent is the container's CPU time used between
// this sample and the previous one, as a share of total system CPU time over the
// same interval, scaled to cores (100% = one core). The first sample only
// establishes the baseline.
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
	usage, system := stats.CPUStats.CPUUsage.TotalUsage, stats.CPUStats.SystemUsage
	if value.havePrev && system > value.prevSystem && usage >= value.prevUsage {
		cpus := stats.CPUStats.OnlineCPUs
		if cpus == 0 {
			cpus = 1
		}
		cpu := float64(usage-value.prevUsage) / float64(system-value.prevSystem) * float64(cpus) * 100
		value.cpuSamples++
		value.cpuTotal += cpu
		if cpu > value.cpuMax {
			value.cpuMax = cpu
		}
	}
	value.prevUsage, value.prevSystem, value.havePrev = usage, system, true

	memory := stats.MemoryStats.Usage
	if cache := stats.MemoryStats.Stats["inactive_file"]; memory >= cache {
		memory -= cache
	}
	value.samples++
	value.memoryTotal += memory
	if memory > value.memoryMax {
		value.memoryMax = memory
	}
}

// finish stops sampling and returns each container's averages and maxima.
func (s *containerSampler) finish() map[string]ContainerMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	results := make(map[string]ContainerMetrics, len(s.values))
	for name, value := range s.values {
		if value.samples == 0 {
			reason := "no Docker stats samples were collected"
			if value.err != nil {
				reason = value.err.Error()
				if name == "postgres" && errors.Is(value.err, errNoContainer) {
					reason = noLocalPostgres
				}
			}
			results[name] = ContainerMetrics{UnavailableReason: reason}
			continue
		}
		memoryMean, memoryMax := value.memoryTotal/uint64(value.samples), value.memoryMax
		metrics := ContainerMetrics{Available: true, Samples: value.samples, MemoryMeanBytes: &memoryMean, MemoryMaxBytes: &memoryMax}
		if value.cpuSamples > 0 {
			cpuMean, cpuMax := value.cpuTotal/float64(value.cpuSamples), value.cpuMax
			metrics.CPUMeanPercent, metrics.CPUMaxPercent = &cpuMean, &cpuMax
		}
		results[name] = metrics
	}
	return results
}
