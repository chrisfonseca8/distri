// gateway-simulator is a stateful stand-in for the parts of the Razorpay API our
// application uses. It exists to test OUR backend under repeatable payment
// scenarios and load; it is not Razorpay and says nothing about Razorpay's
// production behaviour. See docs/razorpay-contract.md for what is documented,
// observed, assumed or injected.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/chrisfonseca08/distri/gateway-simulator/internal/sim"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("gateway-simulator stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := sim.LoadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scheduler := sim.NewScheduler()
	go scheduler.Run(ctx)
	store := sim.NewStore(cfg, scheduler.At)
	webhooks := sim.NewWebhooks(cfg)
	store.SetNotifier(webhooks)
	go webhooks.Run(ctx)
	server := sim.NewServer(cfg, store, scheduler, webhooks)

	slog.Info(
		"gateway-simulator listening",
		"addr", cfg.Addr, "seed", cfg.Seed, "time_scale", cfg.TimeScale,
		"api_latency", cfg.APILatency, "auto_capture", cfg.AutoCapture,
		"enforce_receipt_unique", cfg.EnforceReceiptUnique, "duplicate_pays", cfg.DuplicatePays,
		"webhooks", cfg.WebhookURL != "", "webhook_schedule", cfg.WebhookSchedule, "mix", cfg.ScenarioMix,
	)
	if err := server.Run(ctx, cfg.Addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
