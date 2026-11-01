package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadClientAddsTestRunHeader(t *testing.T) {
	const runID = "cloud-c1-20261101"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Test-Run-ID"); got != runID {
			t.Errorf("X-Test-Run-ID = %q, want %q", got, runID)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := &loadClient{testRunID: runID, http: server.Client(), metrics: newCollector()}
	var response map[string]string
	status, _, transportErr, decodeErr := client.do(context.Background(), http.MethodGet, server.URL, nil, nil, &response)
	if transportErr != nil || decodeErr != nil || status != http.StatusOK {
		t.Fatalf("request failed: status=%d transport=%v decode=%v", status, transportErr, decodeErr)
	}
}
