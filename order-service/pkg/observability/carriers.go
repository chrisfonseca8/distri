package observability

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

type httpCarrier http.Header

func (c httpCarrier) Get(key string) string { return http.Header(c).Get(key) }
func (c httpCarrier) Set(key, value string) { http.Header(c).Set(key, value) }
func (c httpCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// amqpCarrier lets the OpenTelemetry propagator read and write AMQP headers.
type amqpCarrier amqp.Table

func (c amqpCarrier) Get(key string) string {
	if v, ok := c[strings.ToLower(key)].(string); ok {
		return v
	}
	return ""
}
func (c amqpCarrier) Set(key, value string) { c[strings.ToLower(key)] = value }
func (c amqpCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

func propagateOut(ctx context.Context, c propagation.TextMapCarrier) {
	otel.GetTextMapPropagator().Inject(ctx, c)
}

func propagateIn(ctx context.Context, c propagation.TextMapCarrier) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, c)
}

// InjectAMQP writes the correlation fields and trace context into message headers.
// It returns headers (allocating when nil) so callers can assign it directly.
func InjectAMQP(ctx context.Context, headers amqp.Table) amqp.Table {
	if headers == nil {
		headers = amqp.Table{}
	}
	f := FieldsFrom(ctx)
	if f.TestRunID != "" && f.TestRunID != NoTestRun {
		headers[AMQPTestRunID] = f.TestRunID
	}
	if f.SagaID != "" {
		headers[AMQPSagaID] = f.SagaID
	}
	if f.OrderID != "" {
		headers[AMQPOrderID] = f.OrderID
	}
	propagateOut(ctx, amqpCarrier(headers))
	return headers
}

// ExtractAMQP restores the correlation fields and trace context from a delivery's headers.
func ExtractAMQP(ctx context.Context, headers amqp.Table, messageID string) context.Context {
	if headers != nil {
		ctx = propagateIn(ctx, amqpCarrier(headers))
	}
	str := func(key string) string {
		if v, ok := headers[key]; ok && v != nil {
			if s, ok := v.(string); ok {
				return s
			}
			return fmt.Sprint(v)
		}
		return ""
	}
	return WithFields(ctx, Fields{
		TestRunID: str(AMQPTestRunID),
		SagaID:    str(AMQPSagaID),
		OrderID:   str(AMQPOrderID),
		MessageID: messageID,
	})
}

// CorrelationJSON is the outbox-row form of the correlation contract: the
// test run, saga and order ids plus the W3C trace context to resume on publish.
type CorrelationJSON map[string]string

// CorrelationFromContext captures ctx for storage next to an outbox row.
func CorrelationFromContext(ctx context.Context) CorrelationJSON {
	out := CorrelationJSON{}
	f := FieldsFrom(ctx)
	if f.TestRunID != NoTestRun {
		out["test_run_id"] = f.TestRunID
	}
	if f.SagaID != "" {
		out["saga_id"] = f.SagaID
	}
	if f.OrderID != "" {
		out["order_id"] = f.OrderID
	}
	tmp := propagation.MapCarrier{}
	propagateOut(ctx, tmp)
	for k, v := range tmp {
		out[k] = v
	}
	return out
}

// ContextFromCorrelation resumes the correlation stored by CorrelationFromContext.
func ContextFromCorrelation(ctx context.Context, c CorrelationJSON) context.Context {
	if len(c) == 0 {
		return ctx
	}
	ctx = propagateIn(ctx, propagation.MapCarrier(c))
	return WithFields(ctx, Fields{TestRunID: c["test_run_id"], SagaID: c["saga_id"], OrderID: c["order_id"]})
}
