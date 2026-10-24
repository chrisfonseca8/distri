package config

import (
	"errors"
	"fmt"
	"time"
)

const (
	defaultPaymentWindow     = 5 * time.Minute
	defaultReconcileInterval = 15 * time.Second
	defaultReconcileMinPoll  = 10 * time.Second
	defaultReconcileBatch    = 20
	defaultAuditWindow       = time.Hour
	defaultAuditInterval     = time.Minute
)

// GatewayConfig holds the settings of gateways that complete a payment later
// than the call that creates it (Razorpay): how long the customer has to pay and
// how the reconciler polls the gateway for the outcome.
type GatewayConfig struct {
	// PaymentWindow is the fixed time a payment may stay AWAITING_PAYMENT.
	PaymentWindow time.Duration
	// ReconcileInterval is how often the reconciler wakes up.
	ReconcileInterval time.Duration
	// ReconcileMinPoll is the minimum time between two lookups of one attempt.
	ReconcileMinPoll time.Duration
	ReconcileBatch   int
	// AuditWindow and AuditInterval bound the search for captured money on settled
	// (SUCCEEDED or FAILED) attempts: each is looked up every AuditInterval for
	// AuditWindow after it last changed, then never again by polling. Outside the
	// window a late or additional capture is found only if its webhook arrives.
	AuditWindow   time.Duration
	AuditInterval time.Duration
}

func LoadGatewayConfig() (GatewayConfig, error) {
	if err := loadEnvFile(); err != nil {
		return GatewayConfig{}, fmt.Errorf("load .env: %w", err)
	}
	window, err := envDuration("PAYMENT_WINDOW", defaultPaymentWindow)
	if err != nil {
		return GatewayConfig{}, err
	}
	interval, err := envDuration("PAYMENT_RECONCILE_INTERVAL", defaultReconcileInterval)
	if err != nil {
		return GatewayConfig{}, err
	}
	minPoll, err := envDuration("PAYMENT_RECONCILE_MIN_POLL", defaultReconcileMinPoll)
	if err != nil {
		return GatewayConfig{}, err
	}
	batch, err := envInt("PAYMENT_RECONCILE_BATCH", defaultReconcileBatch)
	if err != nil {
		return GatewayConfig{}, err
	}
	auditWindow, err := envDuration("PAYMENT_AUDIT_WINDOW", defaultAuditWindow)
	if err != nil {
		return GatewayConfig{}, err
	}
	auditInterval, err := envDuration("PAYMENT_AUDIT_INTERVAL", defaultAuditInterval)
	if err != nil {
		return GatewayConfig{}, err
	}

	result := GatewayConfig{
		PaymentWindow:     window,
		ReconcileInterval: interval,
		ReconcileMinPoll:  minPoll,
		ReconcileBatch:    batch,
		AuditWindow:       auditWindow,
		AuditInterval:     auditInterval,
	}
	if err := result.Validate(); err != nil {
		return GatewayConfig{}, err
	}
	return result, nil
}

func (c GatewayConfig) Validate() error {
	if c.PaymentWindow <= 0 {
		return errors.New("PAYMENT_WINDOW must be greater than zero")
	}
	if c.ReconcileInterval <= 0 {
		return errors.New("PAYMENT_RECONCILE_INTERVAL must be greater than zero")
	}
	if c.ReconcileMinPoll < 0 {
		return errors.New("PAYMENT_RECONCILE_MIN_POLL must not be negative")
	}
	if c.ReconcileBatch <= 0 {
		return errors.New("PAYMENT_RECONCILE_BATCH must be greater than zero")
	}
	if c.AuditWindow <= 0 || c.AuditInterval <= 0 {
		return errors.New("PAYMENT_AUDIT_WINDOW and PAYMENT_AUDIT_INTERVAL must be greater than zero")
	}
	return nil
}
