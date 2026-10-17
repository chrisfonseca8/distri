package config

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

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

func ServerPort() (string, error) {
	if err := loadEnvFile(); err != nil {
		return "", fmt.Errorf("load .env: %w", err)
	}

	port := os.Getenv("PAYMENT_PORT")
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", fmt.Errorf("PAYMENT_PORT must be a number between 1 and 65535, got %q", port)
	}

	return port, nil
}
