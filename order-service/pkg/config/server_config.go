package config

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

// NewServer builds the HTTP server used by the order service.
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

// ServerPort returns the port used by the HTTP server.
func ServerPort() (string, error) {
	if err := loadEnvFile(); err != nil {
		return "", fmt.Errorf("load .env: %w", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		return "", fmt.Errorf("PORT is not set")
	}

	return port, nil
}



