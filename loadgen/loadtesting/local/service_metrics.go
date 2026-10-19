package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const serviceMetricsPath = "/debug/load-metrics"

func serviceURLs(cfg config) map[string]string {
	return map[string]string{"order": cfg.OrderURL, "inventory": cfg.InventoryURL, "payment": cfg.PaymentURL}
}

func resetServiceMetrics(ctx context.Context, cfg config) error {
	client := &http.Client{Timeout: 3 * time.Second}
	for name, baseURL := range serviceURLs(cfg) {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+serviceMetricsPath+"/reset", nil)
		if err != nil {
			return fmt.Errorf("build %s telemetry reset: %w", name, err)
		}
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("reset %s telemetry: %w", name, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			return fmt.Errorf("reset %s telemetry returned HTTP %d; LOAD_TEST_TELEMETRY must be enabled", name, response.StatusCode)
		}
	}
	return nil
}

func fetchServiceMetrics(ctx context.Context, cfg config, containers map[string]ContainerMetrics) (map[string]ServiceMetrics, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	results := make(map[string]ServiceMetrics, 3)
	for name, baseURL := range serviceURLs(cfg) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+serviceMetricsPath, nil)
		if err != nil {
			return nil, fmt.Errorf("build %s telemetry request: %w", name, err)
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("fetch %s telemetry: %w", name, err)
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("fetch %s telemetry returned HTTP %d", name, response.StatusCode)
		}
		var payload struct {
			Inbound RequestMetrics `json:"inbound"`
			Runtime RuntimeMetrics `json:"runtime"`
			DBPool  DBPoolMetrics  `json:"db_pool"`
		}
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			response.Body.Close()
			return nil, fmt.Errorf("decode %s telemetry: %w", name, err)
		}
		response.Body.Close()
		container := containers[name]
		results[name] = ServiceMetrics{Available: true, Inbound: &payload.Inbound, Runtime: &payload.Runtime, DBPool: &payload.DBPool, Container: &container}
	}
	return results, nil
}
