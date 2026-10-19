package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chrisfonseca08/distri/payment-service/pkg/config"
	"github.com/chrisfonseca08/distri/payment-service/pkg/controllers"
	"github.com/chrisfonseca08/distri/payment-service/pkg/health"
	"github.com/chrisfonseca08/distri/payment-service/pkg/routes"
	"github.com/chrisfonseca08/distri/payment-service/pkg/services"
	"github.com/chrisfonseca08/distri/payment-service/pkg/telemetry"
)

const (
	gatewayTimeout        = 3 * time.Second
	healthCheckTimeout    = 3 * time.Second
	serverShutdownTimeout = 10 * time.Second
	workerShutdownTimeout = 30 * time.Second
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("payment-service stopped", "error", err)
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

	paymentProcessor := services.NewPaymentProcessor(
		applicationConfig.DB,
		applicationConfig.Gateway,
		gatewayTimeout,
	)

	server, err := config.NewServer(newHandler(applicationConfig, paymentProcessor, sqlDB))
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

	serverErrors := startServer(server, listener)
	if err := verifyStartupHealth(applicationConfig, server.Addr); err != nil {
		return errors.Join(err, stopServer(server, paymentProcessor))
	}

	var runErr error
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("serve HTTP: %w", err)
		}
	case <-shutdownSignal.Done():
		slog.Info("shutting down payment-service")
	}

	return errors.Join(runErr, stopServer(server, paymentProcessor))
}

func newHandler(
	applicationConfig *config.Config,
	paymentProcessor *services.PaymentProcessor,
	sqlDB *sql.DB,
) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health.ServerHandler)

	paymentService := services.NewPaymentService(applicationConfig.DB)
	paymentController := controllers.NewPaymentController(paymentService, paymentProcessor)
	routes.RegisterPaymentRoutes(mux, paymentController)

	telemetryRecorder := telemetry.New(sqlDB)
	if telemetry.Enabled() {
		telemetryRecorder.Register(mux)
		return telemetryRecorder.Middleware(mux)
	}
	return mux
}

func startServer(server *http.Server, listener net.Listener) <-chan error {
	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("payment-service listening", "addr", server.Addr)
		serverErrors <- server.Serve(listener)
	}()
	return serverErrors
}

func verifyStartupHealth(applicationConfig *config.Config, serverAddress string) error {
	ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
	defer cancel()

	status := health.Check(
		ctx,
		applicationConfig.DB,
		"http://127.0.0.1"+serverAddress+"/health",
	)
	slog.Info("startup health check", "healthy", status.Healthy, "checks", status.Checks)

	if !status.Healthy {
		return errors.New("startup health check failed")
	}
	return nil
}

func stopServer(server *http.Server, paymentProcessor *services.PaymentProcessor) error {
	shutdownCtx, cancelShutdown := context.WithTimeout(
		context.Background(),
		serverShutdownTimeout,
	)
	serverErr := server.Shutdown(shutdownCtx)
	cancelShutdown()
	if serverErr != nil {
		serverErr = fmt.Errorf("shut down HTTP server: %w", serverErr)
	}

	workerCtx, cancelWorkers := context.WithTimeout(
		context.Background(),
		workerShutdownTimeout,
	)
	workerErr := paymentProcessor.Wait(workerCtx)
	cancelWorkers()
	if workerErr != nil {
		workerErr = fmt.Errorf("wait for in-flight payments: %w", workerErr)
	}

	return errors.Join(serverErr, workerErr)
}
