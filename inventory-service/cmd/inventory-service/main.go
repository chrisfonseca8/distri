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
	"sync"
	"syscall"
	"time"

	"github.com/chrisfonseca08/distri/inventory-service/pkg/config"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/controllers"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/health"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/messages"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/messaging"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/outbox"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/retention"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/routes"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/services"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/telemetry"
)

// main starts the inventory service and exits non-zero if it stops with an error.
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("inventory-service stopped", "error", err)
		os.Exit(1)
	}
}

// run wires up the database, RabbitMQ workers and HTTP server, then serves until shutdown.
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

	shutdownSignal, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stopSignals()

	// Deferred last-in-first-out: cancel the workers, wait for the consumers
	// and relay to finish, then close the publisher they used.
	workerContext, cancelWorkers := context.WithCancel(shutdownSignal)
	var workerWaitGroup sync.WaitGroup
	var outboxPublisher *messaging.Publisher
	defer func() {
		cancelWorkers()
		workerWaitGroup.Wait()
		if outboxPublisher != nil {
			if err := outboxPublisher.Close(); err != nil {
				slog.Error("close outbox publisher", "error", err)
			}
		}
	}()

	rabbitMQConfig, err := config.LoadRabbitMQConfig()
	if err != nil {
		return fmt.Errorf("load RabbitMQ configuration: %w", err)
	}
	outboxPublisher, err = messaging.NewPublisher(rabbitMQConfig.URL)
	if err != nil {
		return fmt.Errorf("set up outbox publisher: %w", err)
	}
	relay, err := outbox.NewRelay(
		outbox.NewStore(applicationConfig.DB),
		outboxPublisher,
		outbox.DefaultRelayConfig(),
	)
	if err != nil {
		return fmt.Errorf("set up outbox relay: %w", err)
	}
	workerWaitGroup.Add(1)
	go func() {
		defer workerWaitGroup.Done()
		relay.Run(workerContext)
	}()

	retentionConfig, err := retention.LoadConfig()
	if err != nil {
		return fmt.Errorf("load retention configuration: %w", err)
	}
	purger := retention.New(applicationConfig.DB, retentionConfig, retention.Rules())
	workerWaitGroup.Add(1)
	go func() {
		defer workerWaitGroup.Done()
		purger.Run(workerContext)
	}()

	inventoryHandler := services.NewInventoryCommandHandler(applicationConfig.DB)
	pool, err := messaging.NewPool(
		messaging.PoolConfig{
			URL:            rabbitMQConfig.URL,
			Queue:          messages.InventoryCommandsQueue,
			Min:            rabbitMQConfig.ConsumerConcurrency,
			Max:            rabbitMQConfig.ConsumerPool.MaxConcurrency,
			Interval:       rabbitMQConfig.ConsumerPool.ScaleInterval,
			DepthPerWorker: rabbitMQConfig.ConsumerPool.DepthPerWorker,
			ScaleDownAfter: rabbitMQConfig.ConsumerPool.ScaleDownAfter,
		},
		func() (*messaging.Consumer, error) {
			return messaging.NewConsumer(
				rabbitMQConfig.URL,
				messages.InventoryCommandsQueue,
				rabbitMQConfig.ConsumerPrefetch,
				inventoryHandler.Handle,
				messaging.WithMaxAttempts(rabbitMQConfig.ConsumerMaxAttempts),
			)
		},
	)
	if err != nil {
		return fmt.Errorf("set up inventory command consumer pool: %w", err)
	}
	workerWaitGroup.Add(1)
	go func() {
		defer workerWaitGroup.Done()
		pool.Run(workerContext)
	}()

	mux := http.NewServeMux()
	productService := services.NewProductService(applicationConfig.DB)
	productController := controllers.NewProductController(productService)
	routes.RegisterProductRoutes(mux, productController)

	reservationService := services.NewReservationService(applicationConfig.DB)
	reservationController := controllers.NewReservationController(reservationService)
	routes.RegisterReservationRoutes(mux, reservationController)
	telemetryRecorder := telemetry.New(sqlDB)
	var handler http.Handler = mux
	if telemetry.Enabled() {
		telemetryRecorder.Register(mux)
		handler = telemetryRecorder.Middleware(mux)
	}

	server, err := config.NewServer(handler)
	if err != nil {
		return fmt.Errorf("set up server: %w", err)
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", server.Addr, err)
	}
	defer listener.Close()

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
