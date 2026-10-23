package main

import (
	"context"
	"database/sql"
	"errors"
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

	"github.com/chrisfonseca08/distri/payment-service/pkg/config"
	"github.com/chrisfonseca08/distri/payment-service/pkg/controllers"
	"github.com/chrisfonseca08/distri/payment-service/pkg/health"
	"github.com/chrisfonseca08/distri/payment-service/pkg/jobs"
	"github.com/chrisfonseca08/distri/payment-service/pkg/messages"
	"github.com/chrisfonseca08/distri/payment-service/pkg/messaging"
	"github.com/chrisfonseca08/distri/payment-service/pkg/outbox"
	"github.com/chrisfonseca08/distri/payment-service/pkg/routes"
	"github.com/chrisfonseca08/distri/payment-service/pkg/services"
	"github.com/chrisfonseca08/distri/payment-service/pkg/telemetry"
)

const (
	gatewayTimeout        = 3 * time.Second
	healthCheckTimeout    = 3 * time.Second
	serverShutdownTimeout = 10 * time.Second
)

type serviceRole string

const (
	roleAPI    serviceRole = "api"
	roleWorker serviceRole = "worker"
	roleAll    serviceRole = "all"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	role := flag.String("role", string(roleAll), "process role: api, worker, or all")
	flag.Parse()

	if err := run(*role); err != nil {
		slog.Error("payment-service stopped", "error", err)
		os.Exit(1)
	}
}

func run(roleValue string) error {
	role, err := parseRole(roleValue)
	if err != nil {
		return err
	}

	applicationConfig, err := config.Setup()
	if err != nil {
		return fmt.Errorf("set up configuration: %w", err)
	}

	sqlDB, err := applicationConfig.DB.DB()
	if err != nil {
		return fmt.Errorf("get database connection: %w", err)
	}
	defer sqlDB.Close()

	shutdownContext, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	var workerWaitGroup sync.WaitGroup
	var outboxPublisher *messaging.Publisher
	defer func() {
		stopSignals()
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

		paymentHandler := services.NewPaymentInitiateHandler(applicationConfig.DB)
		pool, err := messaging.NewPool(
			messaging.PoolConfig{
				URL:            rabbitMQConfig.URL,
				Queue:          messages.PaymentCommandsQueue,
				Min:            rabbitMQConfig.ConsumerConcurrency,
				Max:            rabbitMQConfig.ConsumerPool.MaxConcurrency,
				Interval:       rabbitMQConfig.ConsumerPool.ScaleInterval,
				DepthPerWorker: rabbitMQConfig.ConsumerPool.DepthPerWorker,
				ScaleDownAfter: rabbitMQConfig.ConsumerPool.ScaleDownAfter,
			},
			func() (*messaging.Consumer, error) {
				return messaging.NewConsumer(
					rabbitMQConfig.URL,
					messages.PaymentCommandsQueue,
					rabbitMQConfig.ConsumerPrefetch,
					paymentHandler.Handle,
					messaging.WithMaxAttempts(rabbitMQConfig.ConsumerMaxAttempts),
				)
			},
		)
		if err != nil {
			return fmt.Errorf("set up payment command consumer pool: %w", err)
		}
		workerWaitGroup.Add(1)
		go func() {
			defer workerWaitGroup.Done()
			pool.Run(shutdownContext)
		}()

		processor := services.NewPaymentJobHandler(
			applicationConfig.DB,
			applicationConfig.Gateway,
			gatewayTimeout,
		)
		worker, err := jobs.NewWorker(
			jobs.NewStore(applicationConfig.DB),
			processor,
			applicationConfig.Worker,
		)
		if err != nil {
			return fmt.Errorf("set up payment job worker: %w", err)
		}
		workerWaitGroup.Add(1)
		go func() {
			defer workerWaitGroup.Done()
			worker.Run(shutdownContext)
		}()

		resolverConfig, err := config.LoadResolverConfig()
		if err != nil {
			return fmt.Errorf("load payment resolver configuration: %w", err)
		}
		resolver, err := services.NewPaymentResolver(applicationConfig.DB, resolverConfig.Interval)
		if err != nil {
			return fmt.Errorf("set up payment resolver: %w", err)
		}
		workerWaitGroup.Add(1)
		go func() {
			defer workerWaitGroup.Done()
			resolver.Run(shutdownContext)
		}()
	}

	if role == roleWorker {
		slog.Info("payment-service worker running")
		<-shutdownContext.Done()
		slog.Info("shutting down payment-service worker")
		return nil
	}

	server, err := config.NewServer(newHandler(applicationConfig, sqlDB))
	if err != nil {
		return fmt.Errorf("set up server: %w", err)
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", server.Addr, err)
	}
	defer listener.Close()

	serverErrors := startServer(server, listener)
	if err := verifyStartupHealth(applicationConfig, server.Addr); err != nil {
		return errors.Join(err, stopServer(server))
	}

	var runErr error
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("serve HTTP: %w", err)
		}
	case <-shutdownContext.Done():
		slog.Info("shutting down payment-service")
	}

	return errors.Join(runErr, stopServer(server))
}

func newHandler(applicationConfig *config.Config, sqlDB *sql.DB) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health.ServerHandler)

	paymentService := services.NewPaymentService(applicationConfig.DB)
	paymentController := controllers.NewPaymentController(paymentService)
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

func stopServer(server *http.Server) error {
	shutdownCtx, cancelShutdown := context.WithTimeout(
		context.Background(),
		serverShutdownTimeout,
	)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
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
