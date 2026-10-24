package config

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

const defaultPort = "8083"

// NewServer builds the HTTP server for handler on the configured port.
func NewServer(handler http.Handler) (*http.Server, error) {
	port, err := ServerPort()
	if err != nil {
		return nil, err
	}

	return &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}, nil
}

// ServerPort returns ORCHESTRATOR_PORT, or the default port when it is unset.
func ServerPort() (string, error) {
	if err := loadEnvFile(); err != nil {
		return "", fmt.Errorf("load .env: %w", err)
	}

	if port := os.Getenv("ORCHESTRATOR_PORT"); port != "" {
		return port, nil
	}
	return defaultPort, nil
}
