package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDockerContainerActionThroughUnixSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	var mu sync.Mutex
	var actions []string
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/containers/json":
			if !strings.Contains(r.URL.Query().Get("filters"), "com.docker.compose.service=order-service") {
				t.Errorf("filters = %q", r.URL.Query().Get("filters"))
			}
			_ = json.NewEncoder(w).Encode([]dockerContainer{{ID: "abc123", State: "running"}})
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/containers/abc123/"):
			mu.Lock()
			actions = append(actions, strings.TrimPrefix(r.URL.Path, "/containers/abc123/"))
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := dockerContainerAction(ctx, socket, "stop", "order-service"); err != nil {
		t.Fatal(err)
	}
	if err := dockerContainerAction(ctx, socket, "start", "order-service"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(actions, ",") != "stop,start" {
		t.Fatalf("actions = %v", actions)
	}
}

func TestCorrectnessResetRequested(t *testing.T) {
	t.Setenv("CORRECTNESS_RESET_DATA", "true")
	value, err := correctnessResetRequested()
	if err != nil || !value {
		t.Fatalf("value=%v err=%v", value, err)
	}
	t.Setenv("CORRECTNESS_RESET_DATA", "not-a-boolean")
	if _, err := correctnessResetRequested(); err == nil {
		t.Fatal("invalid reset flag was accepted")
	}
}
