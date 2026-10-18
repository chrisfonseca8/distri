package config

import (
	"errors"
	"net/http"
	"os"
	"time"
)

func NewServer(handler http.Handler) (*http.Server, error) {
	if err := loadEnvFile(); err != nil {
		return nil, err
	}

	port := os.Getenv("INVENTORY_PORT")
	if port == "" {
		return nil, errors.New("INVENTORY_PORT is not set")
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
