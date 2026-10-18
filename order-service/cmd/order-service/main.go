package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chrisfonseca08/distri/order-service/pkg/config"
	"github.com/chrisfonseca08/distri/order-service/pkg/controllers"
	"github.com/chrisfonseca08/distri/order-service/pkg/health"
	"github.com/chrisfonseca08/distri/order-service/pkg/routes"
	"github.com/chrisfonseca08/distri/order-service/pkg/services"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("order-service stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	applicationConfig, err := config.Config_setup()
	if err != nil {
		return fmt.Errorf("set up configuration: %w", err)
	}

	sqlDB, err := applicationConfig.DB.DB()
	if err != nil {
		return fmt.Errorf("get database connection: %w", err)
	}
	defer sqlDB.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health.ServerHandler)

	checkoutService := services.NewCheckoutService(applicationConfig.DB)
	checkoutController := controllers.NewCheckoutController(checkoutService)
	routes.RegisterCheckoutRoutes(mux, checkoutController)

	server, err := config.NewServer(mux)
	if err != nil {
		return fmt.Errorf("set up server: %w", err)
	}

	shutdownContext, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", server.Addr, err)
	}
	defer listener.Close()

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("order-service listening", "addr", server.Addr)
		serverErrors <- server.Serve(listener)
	}()

	healthContext, cancelHealthCheck := context.WithTimeout(context.Background(), 3*time.Second)
	status := health.HealthCheck(
		healthContext,
		applicationConfig.DB,
		"http://127.0.0.1"+server.Addr+"/health",
	)
	cancelHealthCheck()

	slog.Info("health check", "healthy", status.Healthy, "checks", status.Checks)
	if !status.Healthy {
		return fmt.Errorf("startup health check failed")
	}

	select {
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-shutdownContext.Done():
		slog.Info("shutting down order-service")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shut down HTTP server: %w", err)
	}

	return nil
}
