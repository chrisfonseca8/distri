package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chrisfonseca08/distri/inventory-service/pkg/config"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/controllers"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/health"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/routes"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/services"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("inventory-service stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	applicationConfig, err := config.Setup()
	if err != nil {
		return fmt.Errorf("set up configuration: %w", err)
	}

	sqlDB, err := applicationConfig.DB.DB()
	if err != nil {
		return fmt.Errorf("get database connection: %w", err)
	}
	defer sqlDB.Close()

	mux := http.NewServeMux()
	productService := services.NewProductService(applicationConfig.DB)
	productController := controllers.NewProductController(productService)
	routes.RegisterProductRoutes(mux, productController)

	reservationService := services.NewReservationService(applicationConfig.DB)
	reservationController := controllers.NewReservationController(reservationService)
	routes.RegisterReservationRoutes(mux, reservationController)

	server, err := config.NewServer(mux)
	if err != nil {
		return fmt.Errorf("set up server: %w", err)
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", server.Addr, err)
	}
	defer listener.Close()

	shutdownSignal, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stopSignals()

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("inventory-service listening", "addr", server.Addr)
		serverErrors <- server.Serve(listener)
	}()

	healthContext, cancelHealthCheck := context.WithTimeout(context.Background(), 3*time.Second)
	healthErr := health.CheckDatabase(healthContext, applicationConfig.DB)
	cancelHealthCheck()
	if healthErr != nil {
		return fmt.Errorf("startup database health check: %w", healthErr)
	}

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-shutdownSignal.Done():
		slog.Info("shutting down inventory-service")
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shut down HTTP server: %w", err)
	}

	return nil
}
