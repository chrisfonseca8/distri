package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/clients"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/config"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/controllers"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/messaging"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/outbox"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/routes"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/services/checkout"
)

const shutdownTimeout = 10 * time.Second

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("orchestrator-service stopped", "error", err)
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

	inventoryClient, err := clients.NewInventoryClientFromEnv()
	if err != nil {
		return fmt.Errorf("set up inventory client: %w", err)
	}
	sagaConfig, err := config.LoadSagaConfig()
	if err != nil {
		return fmt.Errorf("load saga configuration: %w", err)
	}

	rabbitMQConfig, err := config.LoadRabbitMQConfig()
	if err != nil {
		return fmt.Errorf("load RabbitMQ configuration: %w", err)
	}
	if err := messaging.SetupTopology(rabbitMQConfig.URL); err != nil {
		return fmt.Errorf("set up RabbitMQ topology: %w", err)
	}

	publisher, err := messaging.NewPublisher(rabbitMQConfig.URL)
	if err != nil {
		return fmt.Errorf("set up RabbitMQ publisher: %w", err)
	}
	defer publisher.Close()

	relay, err := outbox.NewRelay(
		outbox.NewStore(applicationConfig.DB),
		publisher,
		outbox.DefaultRelayConfig(),
	)
	if err != nil {
		return fmt.Errorf("set up outbox relay: %w", err)
	}

	mux := http.NewServeMux()
	routes.RegisterHealthRoutes(mux, applicationConfig.DB)
	checkoutService := checkout.NewCheckoutService(
		applicationConfig.DB,
		inventoryClient,
		sagaConfig.Deadline,
		sagaConfig.StepTimeout,
	)
	routes.RegisterCheckoutRoutes(mux, controllers.NewCheckoutController(checkoutService))

	server, err := config.NewServer(mux)
	if err != nil {
		return fmt.Errorf("set up server: %w", err)
	}

	shutdownSignal, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	var backgroundWorkers sync.WaitGroup
	backgroundWorkers.Add(1)
	go func() {
		defer backgroundWorkers.Done()
		relay.Run(shutdownSignal)
	}()
	defer func() {
		stopSignals()
		backgroundWorkers.Wait()
	}()

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("orchestrator-service listening", "addr", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-shutdownSignal.Done():
		slog.Info("shutting down orchestrator-service")
	}

	shutdownContext, cancelShutdown := context.WithTimeout(
		context.Background(),
		shutdownTimeout,
	)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shut down HTTP server: %w", err)
	}
	return nil
}
