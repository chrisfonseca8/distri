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
	"github.com/chrisfonseca08/distri/payment-service/pkg/payment_gateway"
	"github.com/chrisfonseca08/distri/payment-service/pkg/retention"
	"github.com/chrisfonseca08/distri/payment-service/pkg/routes"
	"github.com/chrisfonseca08/distri/payment-service/pkg/services"
	"github.com/chrisfonseca08/distri/payment-service/pkg/telemetry"
	"github.com/chrisfonseca08/distri/payment-service/pkg/webhooks"
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

// main reads the -role flag, starts the service and exits non-zero if it stops with an error.
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	role := flag.String("role", string(roleAll), "process role: api, worker, or all")
	flag.Parse()

	if err := run(*role); err != nil {
		slog.Error("payment-service stopped", "error", err)
		os.Exit(1)
	}
}

// run starts the HTTP API, the background workers or both, depending on role, and serves until shutdown.
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

	webhookConfig, err := config.LoadWebhookConfig()
	if err != nil {
		return fmt.Errorf("load webhook configuration: %w", err)
	}
	var webhookHandler http.Handler
	if webhookConfig.Enabled() {
		handler, err := webhooks.NewHandler(
			webhookConfig.Secret,
			webhookConfig.MaxBodyBytes,
			webhooks.NewGormStore(applicationConfig.DB),
		)
		if err != nil {
			return fmt.Errorf("set up webhook handler: %w", err)
		}
		webhookHandler = handler
	} else {
		slog.Info("webhooks disabled (RAZORPAY_WEBHOOK_SECRET is not set); polling is the only completion path")
	}

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

		retentionConfig, err := retention.LoadConfig()
		if err != nil {
			return fmt.Errorf("load retention configuration: %w", err)
		}
		purger := retention.New(applicationConfig.DB, retentionConfig, retention.Rules())
		workerWaitGroup.Add(1)
		go func() {
			defer workerWaitGroup.Done()
			purger.Run(shutdownContext)
		}()

		gatewayConfig, err := config.LoadGatewayConfig()
		if err != nil {
			return fmt.Errorf("load gateway configuration: %w", err)
		}
		paymentHandler := services.NewPaymentCommandHandler(
			services.NewPaymentInitiateHandler(
				applicationConfig.DB,
				services.WithGatewayName(applicationConfig.GatewayName),
			),
			services.NewPaymentRefundHandler(applicationConfig.DB),
		)
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
			services.WithPaymentWindow(gatewayConfig.PaymentWindow),
			services.WithJobGatewayName(applicationConfig.GatewayName),
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

		// Durable work is routed by its own gateway label: refuse to start if any
		// unsettled work carries a label this build cannot route.
		if err := services.ValidateGatewayLabels(shutdownContext, applicationConfig.DB, applicationConfig.GatewayName); err != nil {
			return fmt.Errorf("validate gateway labels: %w", err)
		}

		// Only gateways that finish a payment later (Razorpay) can be polled; the
		// simulator settles inside CreateCharge and needs no reconciler.
		var compensationWorker *services.CompensationWorker
		if _, pollable := applicationConfig.Gateway.(paymentgateway.OrderLookup); pollable {
			reconciler, err := services.NewPaymentReconciler(
				applicationConfig.DB,
				applicationConfig.Gateway,
				applicationConfig.GatewayName,
				services.ReconcilerConfig{
					Interval:         gatewayConfig.ReconcileInterval,
					MinPoll:          gatewayConfig.ReconcileMinPoll,
					Batch:            gatewayConfig.ReconcileBatch,
					PaymentWindow:    gatewayConfig.PaymentWindow,
					AuditWindow:      gatewayConfig.AuditWindow,
					UnconfirmedGrace: gatewayConfig.UnconfirmedGrace,
					AuditInterval:    gatewayConfig.AuditInterval,
					GatewayTimeout:   gatewayTimeout,
				},
			)
			if err != nil {
				return fmt.Errorf("set up payment reconciler: %w", err)
			}
			workerWaitGroup.Add(1)
			go func() {
				defer workerWaitGroup.Done()
				reconciler.Run(shutdownContext)
			}()

			compensationConfig, err := config.LoadCompensationConfig()
			if err != nil {
				return fmt.Errorf("load compensation configuration: %w", err)
			}
			compensationWorker, err = services.NewCompensationWorker(
				applicationConfig.DB,
				applicationConfig.Gateway,
				applicationConfig.GatewayName,
				services.CompensationConfig{
					Interval:              compensationConfig.Interval,
					Batch:                 compensationConfig.Batch,
					Lease:                 compensationConfig.Lease,
					GatewayTimeout:        gatewayTimeout,
					PendingPoll:           compensationConfig.PendingPoll,
					AbsenceWindow:         compensationConfig.AbsenceWindow,
					IdempotentRetryWindow: compensationConfig.IdempotentRetryWindow,
					MaxIdempotentRetries:  compensationConfig.MaxIdempotentRetries,
					UnknownMaxAge:         compensationConfig.UnknownMaxAge,
					MaxFailedRetries:      compensationConfig.MaxFailedRetries,
					RetryDelay:            compensationConfig.RetryDelay,
					ReviewRetryAfter:      compensationConfig.ReviewRetryAfter,
					MaxReviewRetries:      compensationConfig.MaxReviewRetries,
				},
			)
			if err != nil {
				return fmt.Errorf("set up compensation worker: %w", err)
			}
			workerWaitGroup.Add(1)
			go func() {
				defer workerWaitGroup.Done()
				compensationWorker.Run(shutdownContext)
			}()
		}

		if webhookConfig.Enabled() {
			processor, err := services.NewWebhookProcessor(applicationConfig.DB, services.WebhookProcessorConfig{
				Interval:      webhookConfig.Interval,
				Batch:         webhookConfig.Batch,
				Lease:         webhookConfig.Lease,
				MaxAttempts:   webhookConfig.MaxAttempts,
				BackoffBase:   webhookConfig.BackoffBase,
				BackoffCap:    webhookConfig.BackoffCap,
				PaymentWindow: gatewayConfig.PaymentWindow,
				GatewayName:   applicationConfig.GatewayName,
			})
			if err != nil {
				return fmt.Errorf("set up webhook processor: %w", err)
			}
			processor.WithCompensation(compensationWorker)
			workerWaitGroup.Add(1)
			go func() {
				defer workerWaitGroup.Done()
				processor.Run(shutdownContext)
			}()
		}

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

	server, err := config.NewServer(newHandler(applicationConfig, sqlDB, webhookHandler))
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

// newHandler builds the HTTP router with the payment, webhook, health and telemetry routes.
func newHandler(applicationConfig *config.Config, sqlDB *sql.DB, webhookHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health.ServerHandler)

	paymentService := services.NewPaymentService(
		applicationConfig.DB,
		services.WithGatewayName(applicationConfig.GatewayName),
	)
	paymentController := controllers.NewPaymentController(paymentService)
	routes.RegisterPaymentRoutes(mux, paymentController)
	if webhookHandler != nil {
		routes.RegisterWebhookRoutes(mux, webhookHandler)
	}

	telemetryRecorder := telemetry.New(sqlDB)
	if telemetry.Enabled() {
		telemetryRecorder.Register(mux)
		return telemetryRecorder.Middleware(mux)
	}
	return mux
}

// startServer serves HTTP in the background and returns a channel that receives the server's error.
func startServer(server *http.Server, listener net.Listener) <-chan error {
	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("payment-service listening", "addr", server.Addr)
		serverErrors <- server.Serve(listener)
	}()
	return serverErrors
}

// verifyStartupHealth checks the database and the server's health endpoint before the service reports itself ready.
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

// stopServer shuts the HTTP server down, waiting up to the shutdown timeout for requests in flight.
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

// parseRole checks that the role is api, worker or all.
func parseRole(value string) (serviceRole, error) {
	role := serviceRole(value)
	switch role {
	case roleAPI, roleWorker, roleAll:
		return role, nil
	default:
		return "", fmt.Errorf("invalid role %q: must be api, worker, or all", value)
	}
}
