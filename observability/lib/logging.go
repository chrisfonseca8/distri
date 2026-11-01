package observability

import (
	"context"
	"io"
	"log/slog"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// Stable event names. Logs carry them in the "event" attribute; alerts and
// dashboards (Loki queries) match on these strings, so do not rename them.
const (
	EventSagaStepStarted         = "saga_step_started"
	EventSagaStepCompleted       = "saga_step_completed"
	EventSagaStuck               = "saga_stuck"
	EventOutboxPublishFailed     = "outbox_publish_failed"
	EventOutboxMessageMarkedDead = "outbox_message_marked_dead"
	EventPaymentUnconfirmed      = "payment_unconfirmed"
	EventLateCaptureRefund       = "late_capture_refund_required"
	EventStockInvariantFailed    = "stock_invariant_failed"
)

var (
	sensitiveKey = regexp.MustCompile(`(?i)(password|passwd|secret|token|authorization|api[_-]?key|private[_-]?key|dsn|connection[_-]?string|database_url|rabbitmq_url|signature)`)
	// credentials inside URLs: scheme://user:pass@host
	urlCredentials = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://[^:/@\s]+):[^@\s/]+@`)
	// key=value pairs that leak into error strings (password=..., sslpassword=...)
	kvSecret = regexp.MustCompile(`(?i)\b(password|secret|token|key_secret)=\S+`)
)

const redacted = "[REDACTED]"

// Redact masks credentials in a string.
func Redact(s string) string {
	s = urlCredentials.ReplaceAllString(s, "${1}:"+redacted+"@")
	return kvSecret.ReplaceAllString(s, "${1}="+redacted)
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if sensitiveKey.MatchString(a.Key) && a.Value.Kind() != slog.KindGroup {
		return slog.String(a.Key, redacted)
	}
	switch a.Value.Kind() {
	case slog.KindString:
		if v := a.Value.String(); strings.Contains(v, "://") || strings.Contains(v, "=") {
			return slog.String(a.Key, Redact(v))
		}
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok && err != nil {
			return slog.String(a.Key, Redact(err.Error()))
		}
	}
	return a
}

// ctxHandler adds the process identity, correlation fields and trace ids to each record.
type ctxHandler struct{ slog.Handler }

func (h ctxHandler) Handle(ctx context.Context, r slog.Record) error {
	// Add only what the call site did not already log, so a key never repeats.
	present := map[string]bool{}
	r.Attrs(func(a slog.Attr) bool { present[a.Key] = true; return true })
	add := func(key, value string) {
		if value != "" && !present[key] {
			r.AddAttrs(slog.String(key, value))
		}
	}
	f := FieldsFrom(ctx)
	add("service", serviceName)
	add("environment", environmentName)
	add("test_run_id", f.TestRunID)
	add("order_id", f.OrderID)
	add("saga_id", f.SagaID)
	add("message_id", f.MessageID)
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		add("trace_id", sc.TraceID().String())
		add("span_id", sc.SpanID().String())
	}
	return h.Handler.Handle(ctx, r)
}

func (h ctxHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return ctxHandler{h.Handler.WithAttrs(attrs)}
}
func (h ctxHandler) WithGroup(name string) slog.Handler { return ctxHandler{h.Handler.WithGroup(name)} }

// NewLogger returns the JSON logger every service uses: correlation fields from
// the context are added to each record logged with a *Context method, and secrets
// are redacted.
func NewLogger(w io.Writer) *slog.Logger {
	return slog.New(ctxHandler{slog.NewJSONHandler(w, &slog.HandlerOptions{ReplaceAttr: replaceAttr})})
}

// Event logs a stable-named event: msg is human text, name goes in "event".
func Event(ctx context.Context, level slog.Level, name, msg string, args ...any) {
	slog.Log(ctx, level, msg, append([]any{"event", name}, args...)...)
}
