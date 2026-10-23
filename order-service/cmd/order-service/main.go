package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/chrisfonseca08/distri/order-service/pkg/config"
	"github.com/chrisfonseca08/distri/order-service/pkg/controllers"
	"github.com/chrisfonseca08/distri/order-service/pkg/health"
	"github.com/chrisfonseca08/distri/order-service/pkg/messages"
	"github.com/chrisfonseca08/distri/order-service/pkg/messaging"
	"github.com/chrisfonseca08/distri/order-service/pkg/outbox"
	"github.com/chrisfonseca08/distri/order-service/pkg/routes"
	"github.com/chrisfonseca08/distri/order-service/pkg/services"
	"github.com/chrisfonseca08/distri/order-service/pkg/telemetry"
)

type serviceRole string

const (
	roleAPI    serviceRole = "api"
	roleWorker serviceRole = "worker"
	roleAll    serviceRole = "all"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	role := flag.String("role", string(roleAll), "process role: api, worker, or all")
	flag.Parse()

	if err := run(*role); err != nil {
		slog.Error("order-service stopped", "error", err)
		os.Exit(1)
	}
}

func run(roleValue string) error {
	role, err := parseRole(roleValue)
	if err != nil {
		return err
	}

	applicationConfig, err := config.Config_setup()
	if err != nil {
		return fmt.Errorf("set up configuration: %w", err)
	}

	sqlDB, err := applicationConfig.DB.DB()
	if err != nil {
		return fmt.Errorf("get database connection: %w", err)
	}
	defer sqlDB.Close()

	orderService := services.NewOrderService(applicationConfig.DB)

	shutdownContext, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	var workerWaitGroup sync.WaitGroup
	var outboxPublisher *messaging.Publisher
	defer func() {
		stop()
		workerWaitGroup.Wait()
		if outboxPublisher != nil {
			if err := outboxPublisher.Close(); err != nil {
				slog.Error("close outbox publisher", "error", err)
			}
		}
	}()

	if role == roleWorker || role == roleAll {
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
			relay.Run(shutdownContext)
		}()

		orderCommandHandler := services.NewOrderCommandHandler(applicationConfig.DB)
		pool, err := messaging.NewPool(
			messaging.PoolConfig{
				URL:            rabbitMQConfig.URL,
				Queue:          messages.OrderCommandsQueue,
				Min:            rabbitMQConfig.ConsumerConcurrency,
				Max:            rabbitMQConfig.ConsumerPool.MaxConcurrency,
				Interval:       rabbitMQConfig.ConsumerPool.ScaleInterval,
				DepthPerWorker: rabbitMQConfig.ConsumerPool.DepthPerWorker,
				ScaleDownAfter: rabbitMQConfig.ConsumerPool.ScaleDownAfter,
			},
			func() (*messaging.Consumer, error) {
				return messaging.NewConsumer(
					rabbitMQConfig.URL,
					messages.OrderCommandsQueue,
					orderCommandHandler.Handle,
					messaging.WithMaxAttempts(rabbitMQConfig.ConsumerMaxAttempts),
				)
			},
		)
		if err != nil {
			return fmt.Errorf("set up order command consumer pool: %w", err)
		}
		workerWaitGroup.Add(1)
		go func() {
			defer workerWaitGroup.Done()
			pool.Run(shutdownContext)
		}()
	}

	if role == roleWorker {
		slog.Info("order-service worker running")
		<-shutdownContext.Done()
		slog.Info("shutting down order-service worker")
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health.ServerHandler)
	telemetryRecorder := telemetry.New(sqlDB)
	if telemetry.Enabled() {
		telemetryRecorder.Register(mux)
	}

	orderController := controllers.NewOrderController(orderService)
	routes.RegisterOrderRoutes(mux, orderController)

	var handler http.Handler = mux
	if telemetry.Enabled() {
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

func parseRole(value string) (serviceRole, error) {
	role := serviceRole(value)
	switch role {
	case roleAPI, roleWorker, roleAll:
		return role, nil
	default:
		return "", fmt.Errorf("invalid role %q: must be api, worker, or all", value)
	}
}
