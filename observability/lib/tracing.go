package observability

import (
	"context"
	"os"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "distri"

// InitTracing installs the tracer provider and W3C propagator. Spans are always
// created (so logs carry a trace_id); they are exported only when
// OTEL_EXPORTER_OTLP_ENDPOINT is set. OTEL_TRACES_SAMPLER_ARG (0..1, default 1)
// sets the sampled fraction. The returned function flushes on shutdown.
func InitTracing(ctx context.Context) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	ratio := 1.0
	if v, err := strconv.ParseFloat(os.Getenv("OTEL_TRACES_SAMPLER_ARG"), 64); err == nil && v >= 0 && v <= 1 {
		ratio = v
	}
	host, _ := os.Hostname()
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		"",
		attribute.String("service.name", serviceName),
		attribute.String("deployment.environment.name", environmentName),
		attribute.String("service.instance.id", host),
	))
	if err != nil {
		return nil, err
	}
	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
	}
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "" {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, err
		}
		opts = append(opts, sdktrace.WithBatcher(exporter))
	}
	provider := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}

// Tracer returns the process tracer.
func Tracer() trace.Tracer { return otel.Tracer(tracerName) }

// StartSpan starts a span tagged with the correlation fields in ctx.
func StartSpan(ctx context.Context, name string, kind trace.SpanKind, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	f := FieldsFrom(ctx)
	base := []attribute.KeyValue{attribute.String("test_run_id", f.TestRunID)}
	if f.OrderID != "" {
		base = append(base, attribute.String("order_id", f.OrderID))
	}
	if f.SagaID != "" {
		base = append(base, attribute.String("saga_id", f.SagaID))
	}
	if f.MessageID != "" {
		base = append(base, attribute.String("message_id", f.MessageID))
	}
	return Tracer().Start(ctx, name, trace.WithSpanKind(kind), trace.WithAttributes(append(base, attrs...)...))
}

// EndSpan records err (when not nil) and ends the span.
func EndSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// TraceIDFrom returns the hex trace id in ctx, or "".
func TraceIDFrom(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID().String()
	}
	return ""
}
