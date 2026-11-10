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

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/businessmetrics"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/clients"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/config"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/controllers"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/messages"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/messaging"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/observability"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/outbox"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/poolscaler"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/retention"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/routes"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/services/checkout"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/services/reply"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/statusstream"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/sweeper"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/telemetry"
)

const shutdownTimeout = 10 * time.Second

// main starts the orchestrator service and exits non-zero if it stops with an error.
func main() {
	observability.SetProcess(observability.Config{Service: "orchestrator-service"})
	slog.SetDefault(observability.NewLogger(os.Stdout))

	if err := run(); err != nil {
		slog.Error("orchestrator-service stopped", "error", err)
		os.Exit(1)
	}
}

// run wires up the database, RabbitMQ workers, sweepers and HTTP server, then serves until shutdown.
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

	stopObservability, err := observability.Setup(observability.Options{
		Service: "orchestrator-service", MetricsAddr: "127.0.0.1:9091", DB: applicationConfig.DB, PoolName: "orchestrator",
	})
	if err != nil {
		return fmt.Errorf("set up observability: %w", err)
	}
	defer stopObservability()
	businessmetrics.Register(sqlDB)
	var telemetryRecorder *telemetry.Recorder
	if telemetry.Enabled() {
		telemetryRecorder = telemetry.New(sqlDB)
	}

	// Optional: resize the database/sql pool at runtime (off unless
	// ORCHESTRATOR_DB_POOL_AUTOSCALE=true). Built here so a bad setting fails the start.
	poolScalerConfig, err := config.LoadPoolScalerConfig()
	if err != nil {
		return fmt.Errorf("load database pool autoscaler configuration: %w", err)
	}
	var poolScaler *poolscaler.Scaler
	if poolScalerConfig.Enabled {
		probeDB, err := config.OpenProbeDB()
		if err != nil {
			return fmt.Errorf("open database probe: %w", err)
		}
		defer probeDB.Close()
		guard := poolscaler.NewDBGuard(probeDB, poolScalerConfig, poolscaler.ConnectionErrorCounter(observability.Registry))
		poolScaler, err = poolscaler.New(sqlDB, guard, poolScalerConfig)
		if err != nil {
			return fmt.Errorf("set up database pool autoscaler: %w", err)
		}
		poolscaler.RegisterMetrics()
	}

	inventoryClient, err := clients.NewHTTPInventoryClientFromEnv()
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

	outboxConcurrency, err := config.LoadOutboxConcurrency()
	if err != nil {
		return fmt.Errorf("load outbox concurrency: %w", err)
	}
	outboxPollInterval, err := config.LoadOutboxPollInterval()
	if err != nil {
		return fmt.Errorf("load outbox poll interval: %w", err)
	}
	relayConfig := outbox.DefaultRelayConfig()
	relayConfig.PollInterval = outboxPollInterval
	outboxPublishers := make([]*messaging.Publisher, 0, outboxConcurrency)
	outboxRelays := make([]*outbox.Relay, 0, outboxConcurrency)
	defer func() {
		for _, publisher := range outboxPublishers {
			if err := publisher.Close(); err != nil {
				slog.Error("close outbox publisher", "error", err)
			}
		}
	}()
	for range outboxConcurrency {
		publisher, err := messaging.NewPublisher(rabbitMQConfig.URL)
		if err != nil {
			return fmt.Errorf("set up RabbitMQ outbox publisher: %w", err)
		}
		outboxPublishers = append(outboxPublishers, publisher)
		relay, err := outbox.NewRelay(
			outbox.NewStore(applicationConfig.DB),
			publisher,
			relayConfig,
			outbox.WithTelemetry(telemetryRecorder),
		)
		if err != nil {
			return fmt.Errorf("set up outbox relay: %w", err)
		}
		outboxRelays = append(outboxRelays, relay)
	}
	statusHub := statusstream.NewHub(10_000)
	replyHandler := reply.NewHandler(
		applicationConfig.DB,
		sagaConfig.StepTimeout,
		reply.WithSagaDeadline(sagaConfig.Deadline),
		reply.WithTelemetry(telemetryRecorder),
		reply.WithStatusNotifier(statusHub),
	)
	sagaSweeper, err := sweeper.NewSagaSweeper(applicationConfig.DB, sweeper.SagaConfig{
		Interval:         sagaConfig.SweepInterval,
		Batch:            sagaConfig.SweepBatch,
		StepTimeout:      sagaConfig.StepTimeout,
		StepMaxAttempts:  sagaConfig.StepMaxAttempts,
		ResendBackoffCap: sagaConfig.ResendBackoffCap,
		StuckRefundAfter: sagaConfig.StuckRefundAfter,
		Deadline:         sagaConfig.Deadline,
		UndoReviveAfter:  sagaConfig.UndoReviveAfter,
		UndoMaxRevives:   sagaConfig.UndoMaxRevives,
	}, sweeper.WithStepFailer(replyHandler), sweeper.WithStatusNotifier(statusHub))
	if err != nil {
		return fmt.Errorf("set up saga sweeper: %w", err)
	}
	reservationSweeperConfig, err := config.LoadReservationSweeperConfig()
	if err != nil {
		return fmt.Errorf("load reservation sweeper configuration: %w", err)
	}
	reservationSweeper, err := sweeper.NewReservationSweeper(
		inventoryClient,
		applicationConfig.DB,
		reservationSweeperConfig,
	)
	if err != nil {
		return fmt.Errorf("set up reservation sweeper: %w", err)
	}
	replyConsumers, err := messaging.NewPool(
		messaging.PoolConfig{
			URL:            rabbitMQConfig.URL,
			Queue:          messages.OrchestratorRepliesQueue,
			Min:            rabbitMQConfig.ConsumerConcurrency,
			Max:            rabbitMQConfig.ConsumerPool.MaxConcurrency,
			Interval:       rabbitMQConfig.ConsumerPool.ScaleInterval,
			DepthPerWorker: rabbitMQConfig.ConsumerPool.DepthPerWorker,
			ScaleDownAfter: rabbitMQConfig.ConsumerPool.ScaleDownAfter,
		},
		func() (*messaging.Consumer, error) {
			return messaging.NewConsumer(
				rabbitMQConfig.URL,
				messages.OrchestratorRepliesQueue,
				replyHandler.Handle,
				messaging.WithMaxAttempts(rabbitMQConfig.ConsumerMaxAttempts),
			)
		},
	)
	if err != nil {
		return fmt.Errorf("set up order reply consumer pool: %w", err)
	}

	mux := http.NewServeMux()
	routes.RegisterHealthRoutes(mux, applicationConfig.DB)
	if telemetryRecorder != nil {
		telemetryRecorder.Register(mux)
	}
	checkoutService := checkout.NewCheckoutService(
		applicationConfig.DB,
		inventoryClient,
		sagaConfig.Deadline,
		sagaConfig.StepTimeout,
	)
	routes.RegisterCheckoutRoutes(mux, controllers.NewCheckoutController(checkoutService, statusHub))

	var handler http.Handler = mux
	if telemetryRecorder != nil {
		handler = telemetryRecorder.Middleware(mux)
	}
	handler = observability.HTTPMiddleware(handler)
	server, err := config.NewServer(handler)
	if err != nil {
		return fmt.Errorf("set up server: %w", err)
	}

	shutdownSignal, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	var backgroundWorkers sync.WaitGroup
	retentionConfig, err := retention.LoadConfig()
	if err != nil {
		return fmt.Errorf("load retention configuration: %w", err)
	}
	purger := retention.New(applicationConfig.DB, retentionConfig, retention.Rules())
	if poolScaler != nil {
		backgroundWorkers.Add(1)
		go func() {
			defer backgroundWorkers.Done()
			poolScaler.Run(shutdownSignal)
		}()
	}
	backgroundWorkers.Add(4 + len(outboxRelays))
	go func() {
		defer backgroundWorkers.Done()
		purger.Run(shutdownSignal)
	}()
	for _, relay := range outboxRelays {
		go func() {
			defer backgroundWorkers.Done()
			relay.Run(shutdownSignal)
		}()
	}
	go func() {
		defer backgroundWorkers.Done()
		replyConsumers.Run(shutdownSignal)
	}()
	go func() {
		defer backgroundWorkers.Done()
		sagaSweeper.Run(shutdownSignal)
	}()
	go func() {
		defer backgroundWorkers.Done()
		reservationSweeper.Run(shutdownSignal)
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
