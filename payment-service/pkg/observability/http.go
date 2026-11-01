package observability

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusRecorder) WriteHeader(code int) {
	if !w.wrote {
		w.status, w.wrote = code, true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status, w.wrote = http.StatusOK, true
	}
	return w.ResponseWriter.Write(b)
}

// Flush keeps server-sent events working through the middleware.
func (w *statusRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("hijack not supported")
}

func (w *statusRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// skipInstrumentation lists probe paths that would drown out real traffic.
func skipInstrumentation(path string) bool {
	switch path {
	case "/health", "/healthz", "/readyz", "/metrics":
		return true
	}
	return false
}

// HTTPMiddleware is the outermost handler of every service. It resumes the caller's
// trace and X-Test-Run-ID, echoes the test run id, opens the request span, and
// records the RED metrics under the ServeMux route pattern (never the raw path,
// which would make the label unbounded).
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := ExtractHTTP(r.Context(), r.Header)
		w.Header().Set(HeaderTestRunID, FieldsFrom(ctx).TestRunID)
		if skipInstrumentation(r.URL.Path) {
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		ctx, span := StartSpan(ctx, "HTTP "+r.Method, trace.SpanKindServer,
			attribute.String("http.request.method", r.Method), attribute.String("url.path", r.URL.Path))
		req := r.WithContext(ctx)
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		HTTPInFlight.Inc()
		started := time.Now()

		defer func() {
			HTTPInFlight.Dec()
			route := req.Pattern
			if route == "" {
				route = "unmatched"
			}
			class := strconv.Itoa(rec.status/100) + "xx"
			if rv := recover(); rv != nil {
				class = "5xx"
				Errors.WithLabelValues("http", "panic").Inc()
				span.SetAttributes(attribute.Bool("panic", true))
				span.End()
				HTTPRequests.WithLabelValues(route, r.Method, class).Inc()
				panic(rv)
			}
			HTTPRequests.WithLabelValues(route, r.Method, class).Inc()
			HTTPDuration.WithLabelValues(route, r.Method).Observe(time.Since(started).Seconds())
			if rec.status >= 500 {
				Errors.WithLabelValues("http", "5xx").Inc()
			}
			span.SetAttributes(attribute.String("http.route", route), attribute.Int("http.response.status_code", rec.status))
			span.SetName(route)
			if rec.status >= 500 {
				EndSpan(span, errors.New("http "+strconv.Itoa(rec.status)))
			} else {
				span.End()
			}
		}()
		next.ServeHTTP(rec, req)
	})
}

// roundTripper injects correlation and trace headers into outbound calls and records client metrics.
type roundTripper struct {
	base http.RoundTripper
	peer string
}

// NewHTTPTransport wraps base (nil means http.DefaultTransport). peer is a short,
// bounded name such as "inventory-service" or "razorpay".
func NewHTTPTransport(base http.RoundTripper, peer string) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return roundTripper{base: base, peer: peer}
}

func (t roundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx, span := StartSpan(r.Context(), "HTTP "+r.Method+" "+t.peer, trace.SpanKindClient,
		attribute.String("peer.service", t.peer), attribute.String("http.request.method", r.Method))
	req := r.Clone(ctx)
	InjectHTTP(ctx, req.Header)
	started := time.Now()
	resp, err := t.base.RoundTrip(req)
	HTTPClientDuration.WithLabelValues(t.peer).Observe(time.Since(started).Seconds())
	if err != nil {
		HTTPClientRequests.WithLabelValues(t.peer, "error").Inc()
		Errors.WithLabelValues("http_client_"+t.peer, "transport").Inc()
		EndSpan(span, err)
		return nil, err
	}
	HTTPClientRequests.WithLabelValues(t.peer, strconv.Itoa(resp.StatusCode/100)+"xx").Inc()
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
	if resp.StatusCode >= 500 {
		EndSpan(span, errors.New("http "+strconv.Itoa(resp.StatusCode)))
	} else {
		span.End()
	}
	return resp, nil
}
