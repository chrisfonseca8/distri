package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	amqp "github.com/rabbitmq/amqp091-go"
)

func TestCorrelationSurvivesHTTPAndAMQP(t *testing.T) {
	SetProcess(Config{Service: "test", Environment: "ci"})
	if _, err := InitTracing(context.Background()); err != nil {
		t.Fatal(err)
	}
	InitMetrics()

	var seen Fields
	var traceID string
	handler := HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = FieldsFrom(r.Context())
		traceID = TraceIDFrom(r.Context())
		ctx := WithOrderID(r.Context(), "o-1")
		headers := InjectAMQP(ctx, nil)
		if headers[AMQPTestRunID] != "run-7" || headers[AMQPOrderID] != "o-1" || headers["traceparent"] == nil {
			t.Errorf("AMQP headers incomplete: %v", headers)
		}
		back := ExtractAMQP(context.Background(), amqp.Table(headers), "m-1")
		if f := FieldsFrom(back); f.TestRunID != "run-7" || f.OrderID != "o-1" || f.MessageID != "m-1" {
			t.Errorf("AMQP round trip lost fields: %+v", f)
		}
		if TraceIDFrom(back) != traceID {
			t.Errorf("trace id changed across AMQP: %s != %s", TraceIDFrom(back), traceID)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	mux := http.NewServeMux()
	mux.Handle("POST /checkout", handler)
	req := httptest.NewRequest("POST", "/checkout", nil)
	req.Header.Set(HeaderTestRunID, "run-7")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if seen.TestRunID != "run-7" || rec.Header().Get(HeaderTestRunID) != "run-7" {
		t.Fatalf("test run id not propagated: %+v / %q", seen, rec.Header().Get(HeaderTestRunID))
	}
	if n := testutil.CollectAndCount(HTTPRequests); n != 1 {
		t.Fatalf("expected 1 request series, got %d", n)
	}
}

func TestLoggerAddsCorrelationAndRedactsSecrets(t *testing.T) {
	SetProcess(Config{Service: "test", Environment: "ci"})
	var buf bytes.Buffer
	logger := NewLogger(&buf)
	ctx := WithFields(context.Background(), Fields{TestRunID: "run-9", OrderID: "o-9", SagaID: "s-9"})
	logger.InfoContext(ctx, "x", "event", EventSagaStepStarted, "password", "hunter2",
		"url", "amqp://user:pw123@rabbit:5672/", "error", http.ErrAbortHandler)
	line := buf.String()
	for _, secret := range []string{"hunter2", "pw123"} {
		if strings.Contains(line, secret) {
			t.Fatalf("secret %q leaked: %s", secret, line)
		}
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"test_run_id": "run-9", "order_id": "o-9", "saga_id": "s-9", "service": "test", "event": "saga_step_started"} {
		if rec[k] != want {
			t.Errorf("%s = %v, want %s", k, rec[k], want)
		}
	}
	_ = slog.LevelInfo
}

func TestCorrelationJSONRoundTrip(t *testing.T) {
	SetProcess(Config{Service: "test"})
	ctx := WithFields(context.Background(), Fields{TestRunID: "run-3", SagaID: "s-3"})
	stored := CorrelationFromContext(ctx)
	back := ContextFromCorrelation(context.Background(), stored)
	if f := FieldsFrom(back); f.TestRunID != "run-3" || f.SagaID != "s-3" {
		t.Fatalf("lost: %+v", f)
	}
}
