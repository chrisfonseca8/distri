// Package observability is the shared correlation, logging, metrics and tracing
// toolkit. The canonical copy lives in observability/lib and is copied into each
// service's pkg/observability by observability/sync-observability.sh (each service is
// its own Go module, so it cannot import a sibling directory).
package observability

import (
	"context"
	"net/http"
	"os"
	"strings"
)

// Correlation contract. trace_id comes from the OpenTelemetry span; the rest are
// carried in context, HTTP headers, AMQP headers and outbox rows.
const (
	HeaderTestRunID = "X-Test-Run-ID"
	HeaderSagaID    = "X-Saga-ID"
	HeaderOrderID   = "X-Order-ID"

	// AMQP header keys are lower case (the broker is case sensitive).
	AMQPTestRunID = "x-test-run-id"
	AMQPSagaID    = "x-saga-id"
	AMQPOrderID   = "x-order-id"

	// NoTestRun is the test_run_id of traffic that did not carry one.
	NoTestRun = "none"
)

// Fields are the per-request correlation identifiers. Empty means unknown.
type Fields struct {
	TestRunID string
	SagaID    string
	OrderID   string
	MessageID string
}

type fieldsKey struct{}

var (
	serviceName      = "unknown"
	environmentName  = "local"
	defaultTestRunID = NoTestRun
)

// Config identifies the process in every log line, metric and trace.
type Config struct {
	Service     string
	Environment string
}

// SetProcess records the service and environment names. Environment falls back to
// the ENVIRONMENT variable and then "local"; TEST_RUN_ID, when set, labels work
// that no request or message caused (sweepers, reconcilers).
func SetProcess(cfg Config) {
	if cfg.Service != "" {
		serviceName = cfg.Service
	}
	switch {
	case cfg.Environment != "":
		environmentName = cfg.Environment
	case os.Getenv("ENVIRONMENT") != "":
		environmentName = os.Getenv("ENVIRONMENT")
	}
	if id := strings.TrimSpace(os.Getenv("TEST_RUN_ID")); id != "" {
		defaultTestRunID = id
	}
}

// Service returns the service name.
func Service() string { return serviceName }

// Environment returns the environment name.
func Environment() string { return environmentName }

// WithFields merges the non-empty fields into ctx, keeping what is already set.
func WithFields(ctx context.Context, f Fields) context.Context {
	cur := FieldsFrom(ctx)
	if f.TestRunID != "" {
		cur.TestRunID = f.TestRunID
	}
	if f.SagaID != "" {
		cur.SagaID = f.SagaID
	}
	if f.OrderID != "" {
		cur.OrderID = f.OrderID
	}
	if f.MessageID != "" {
		cur.MessageID = f.MessageID
	}
	return context.WithValue(ctx, fieldsKey{}, cur)
}

// FieldsFrom returns the correlation fields in ctx. TestRunID is never empty.
func FieldsFrom(ctx context.Context) Fields {
	f, _ := ctx.Value(fieldsKey{}).(Fields)
	if f.TestRunID == "" {
		f.TestRunID = defaultTestRunID
	}
	return f
}

// WithOrderID tags ctx with an order id. When no saga id is known the orchestrator
// uses the order id as the saga id, so both are set.
func WithOrderID(ctx context.Context, orderID string) context.Context {
	return WithFields(ctx, Fields{OrderID: orderID})
}

// WithSagaID tags ctx with a saga id.
func WithSagaID(ctx context.Context, sagaID string) context.Context {
	return WithFields(ctx, Fields{SagaID: sagaID})
}

// WithMessageID tags ctx with a message id.
func WithMessageID(ctx context.Context, messageID string) context.Context {
	return WithFields(ctx, Fields{MessageID: messageID})
}

// WithTestRunID tags ctx with a test run id.
func WithTestRunID(ctx context.Context, id string) context.Context {
	return WithFields(ctx, Fields{TestRunID: id})
}

// InjectHTTP copies the correlation fields and the trace context onto outgoing headers.
func InjectHTTP(ctx context.Context, h http.Header) {
	f := FieldsFrom(ctx)
	if f.TestRunID != "" && f.TestRunID != NoTestRun {
		h.Set(HeaderTestRunID, f.TestRunID)
	}
	if f.SagaID != "" {
		h.Set(HeaderSagaID, f.SagaID)
	}
	if f.OrderID != "" {
		h.Set(HeaderOrderID, f.OrderID)
	}
	propagateOut(ctx, httpCarrier(h))
}

// ExtractHTTP reads correlation fields and the trace context from incoming headers.
func ExtractHTTP(ctx context.Context, h http.Header) context.Context {
	ctx = propagateIn(ctx, httpCarrier(h))
	return WithFields(ctx, Fields{
		TestRunID: strings.TrimSpace(h.Get(HeaderTestRunID)),
		SagaID:    strings.TrimSpace(h.Get(HeaderSagaID)),
		OrderID:   strings.TrimSpace(h.Get(HeaderOrderID)),
	})
}
