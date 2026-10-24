package sim

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"
)

const maxBodyBytes = 1 << 20

// latencyCounters bound the memory of the per-operation latency sequence: the
// k-th request for an operation key draws its k-th latency sample from
// hash(seed, key, k). Colliding keys share a counter, which only shifts which
// sample they draw — the model stays seeded and bounded.
const latencyCounters = 1 << 16

type Server struct {
	cfg       Config
	store     *Store
	scheduler *Scheduler
	webhooks  *Webhooks
	counters  [latencyCounters]atomic.Uint64

	inFlight atomic.Int64
	requests atomic.Int64
	byStatus [6]atomic.Int64 // index = status/100
	started  time.Time
}

func NewServer(cfg Config, store *Store, scheduler *Scheduler, webhooks *Webhooks) *Server {
	return &Server{cfg: cfg, store: store, scheduler: scheduler, webhooks: webhooks, started: time.Now()}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/orders", s.api(s.createOrder))
	mux.HandleFunc("GET /v1/orders", s.api(s.listOrders))
	mux.HandleFunc("GET /v1/orders/{id}", s.api(s.getOrder))
	mux.HandleFunc("GET /v1/orders/{id}/payments", s.api(s.orderPayments))
	mux.HandleFunc("GET /v1/payments/{id}", s.api(s.getPayment))
	mux.HandleFunc("POST /v1/payments/{id}/capture", s.api(s.capturePayment))
	mux.HandleFunc("POST /v1/payments/{id}/refund", s.api(s.createRefund))
	mux.HandleFunc("GET /v1/payments/{id}/refunds", s.api(s.paymentRefunds))
	mux.HandleFunc("GET /v1/refunds/{id}", s.api(s.getRefund))
	mux.HandleFunc("GET /v1/refunds", s.api(s.listRefunds))

	// Scenario controls live outside the business API and are not authenticated.
	mux.HandleFunc("PUT /_sim/scenario/{receipt}", s.setScenario)
	mux.HandleFunc("PUT /_sim/default-scenario", s.setDefaultScenario)
	mux.HandleFunc("PUT /_sim/order-scenario/{orderID}", s.setOrderScenario)
	mux.HandleFunc("PUT /_sim/plan/{receipt}", s.setPlan)
	mux.HandleFunc("PUT /_sim/order-plan/{orderID}", s.setOrderPlan)
	mux.HandleFunc("GET /_sim/webhooks", s.listWebhooks)
	mux.HandleFunc("POST /_sim/webhooks/{eventID}/redeliver", s.redeliverWebhook)
	mux.HandleFunc("GET /_sim/metrics", s.metrics)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	return mux
}

// api wraps a /v1 handler with request accounting, authentication and the API
// latency model. Latency is applied before the handler, so a client that gives up
// during the delay changes no gateway state.
func (s *Server) api(handler func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.inFlight.Add(1)
		defer s.inFlight.Add(-1)
		s.requests.Add(1)
		recorder := &statusRecorder{ResponseWriter: w, status: 200}

		if !s.authorized(r) {
			writeError(recorder, &APIError{
				Status: http.StatusUnauthorized, Code: "BAD_REQUEST_ERROR", Description: "Authentication failed",
				Source: "business", Step: "payment_initiation", Reason: "input_validation_failed",
			})
		} else if err := s.delay(r); err != nil {
			// client went away before the gateway acted: no state change
			recorder.status = 499
		} else {
			handler(recorder, r)
		}
		if class := recorder.status / 100; class >= 0 && class < len(s.byStatus) {
			s.byStatus[class].Add(1)
		}
	}
}

func (s *Server) authorized(r *http.Request) bool {
	user, password, ok := r.BasicAuth()
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(user), []byte(s.cfg.KeyID)) == 1 &&
		subtle.ConstantTimeCompare([]byte(password), []byte(s.cfg.KeySecret)) == 1
}

// delay sleeps for the seeded API latency of this operation. The median per
// profile is an assumption (zero, 60 ms, 800 ms); Razorpay's real distribution is
// not documented.
func (s *Server) delay(r *http.Request) error {
	var median time.Duration
	switch s.cfg.APILatency {
	case "normal":
		median = 60 * time.Millisecond
	case "slow":
		median = 800 * time.Millisecond
	default:
		return nil
	}
	key := r.Method + " " + r.URL.Path + "?" + r.URL.RawQuery
	counter := &s.counters[hashParts(s.cfg.Seed, key)%latencyCounters]
	k := counter.Add(1)
	d := lognormal(median, 0.5, s.cfg.Seed, "api-latency", key, strconv.FormatUint(k, 10))
	if d > 10*time.Second {
		d = 10 * time.Second
	}
	d = s.cfg.scaled(d)
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-r.Context().Done():
		return r.Context().Err()
	}
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	var req OrderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, badRequest("The request body is not valid JSON.", ""))
		return
	}
	order, apiErr := s.store.CreateOrder(req)
	respond(w, order, apiErr)
}

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	count, skip := 0, 0
	var err error
	if v := query.Get("count"); v != "" {
		if count, err = strconv.Atoi(v); err != nil {
			writeError(w, badRequest("count must be an integer", "count"))
			return
		}
	}
	if v := query.Get("skip"); v != "" {
		if skip, err = strconv.Atoi(v); err != nil {
			writeError(w, badRequest("skip must be an integer", "skip"))
			return
		}
	}
	items, apiErr := s.store.ListOrders(query.Get("receipt"), count, skip)
	if apiErr != nil {
		writeError(w, apiErr)
		return
	}
	writeJSON(w, http.StatusOK, collection(items))
}

func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	order, apiErr := s.store.GetOrder(r.PathValue("id"))
	respond(w, order, apiErr)
}

func (s *Server) orderPayments(w http.ResponseWriter, r *http.Request) {
	items, apiErr := s.store.OrderPayments(r.PathValue("id"))
	if apiErr != nil {
		writeError(w, apiErr)
		return
	}
	writeJSON(w, http.StatusOK, collection(items))
}

func (s *Server) getPayment(w http.ResponseWriter, r *http.Request) {
	payment, apiErr := s.store.GetPayment(r.PathValue("id"))
	respond(w, payment, apiErr)
}

func (s *Server) capturePayment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Amount   json.Number `json:"amount"`
		Currency string      `json:"currency"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, badRequest("The request body is not valid JSON.", ""))
		return
	}
	payment, apiErr := s.store.CapturePayment(r.PathValue("id"), req.Amount, req.Currency)
	respond(w, payment, apiErr)
}

func (s *Server) createRefund(w http.ResponseWriter, r *http.Request) {
	var req RefundRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, badRequest("The request body is not valid JSON.", ""))
		return
	}
	req.IdempotencyKey = r.Header.Get("X-Refund-Idempotency")
	refund, behavior, apiErr := s.store.CreateRefundWithBehavior(r.PathValue("id"), req)
	if apiErr != nil {
		writeError(w, apiErr)
		return
	}
	// The refund is already committed. What follows only shapes what this caller
	// sees: a client that times out or disconnects does not undo it.
	if behavior.Delay > 0 {
		time.Sleep(behavior.Delay) // deliberately not tied to the request context
		s.store.ReleaseRefundKey(behavior.keyToClear)
	}
	switch {
	case behavior.Lost:
		if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
			_ = conn.Close()
		} else {
			writeError(w, &APIError{Status: http.StatusBadGateway, Code: "SERVER_ERROR", Description: "could not drop the connection: " + err.Error()})
		}
	case behavior.Error500:
		writeError(w, &APIError{Status: http.StatusInternalServerError, Code: "SERVER_ERROR",
			Description: "An error occurred after the refund was created.", Source: "internal", Step: "refund_initiation", Reason: "server_error"})
	default:
		writeJSON(w, http.StatusOK, refund)
	}
}

func (s *Server) paymentRefunds(w http.ResponseWriter, r *http.Request) {
	items, apiErr := s.store.ListPaymentRefunds(r.PathValue("id"))
	if apiErr != nil {
		writeError(w, apiErr)
		return
	}
	writeJSON(w, http.StatusOK, collection(items))
}

func (s *Server) getRefund(w http.ResponseWriter, r *http.Request) {
	refund, apiErr := s.store.GetRefund(r.PathValue("id"))
	respond(w, refund, apiErr)
}

func (s *Server) listRefunds(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	atoi := func(name string) int {
		n, _ := strconv.Atoi(query.Get(name))
		return n
	}
	items, apiErr := s.store.ListRefunds(atoi("count"), atoi("skip"), int64(atoi("from")), int64(atoi("to")))
	if apiErr != nil {
		writeError(w, apiErr)
		return
	}
	writeJSON(w, http.StatusOK, collection(items))
}

func (s *Server) setScenario(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SetScenario(r.PathValue("receipt"), r.URL.Query().Get("name")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"receipt": r.PathValue("receipt"), "scenario": r.URL.Query().Get("name")})
}

func (s *Server) setDefaultScenario(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SetDefaultScenario(r.URL.Query().Get("name")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"default_scenario": r.URL.Query().Get("name")})
}

func (s *Server) setOrderScenario(w http.ResponseWriter, r *http.Request) {
	if apiErr := s.store.SetOrderScenario(r.PathValue("orderID"), r.URL.Query().Get("name")); apiErr != nil {
		writeError(w, apiErr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"order_id": r.PathValue("orderID"), "scenario": r.URL.Query().Get("name")})
}

func (s *Server) setPlan(w http.ResponseWriter, r *http.Request) {
	var plan Plan
	if err := decodeJSON(r, &plan); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.store.SetPlan(r.PathValue("receipt"), plan); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"receipt": r.PathValue("receipt"), "attempts": len(plan.Attempts)})
}

func (s *Server) setOrderPlan(w http.ResponseWriter, r *http.Request) {
	var plan Plan
	if err := decodeJSON(r, &plan); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if apiErr := s.store.SetOrderPlan(r.PathValue("orderID"), plan); apiErr != nil {
		writeError(w, apiErr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"order_id": r.PathValue("orderID"), "attempts": len(plan.Attempts)})
}

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}
	writeJSON(w, http.StatusOK, collection(s.webhooks.List(limit, r.URL.Query().Get("payment_id"))))
}

func (s *Server) redeliverWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.webhooks.Redeliver(r.PathValue("eventID")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown event"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"event_id": r.PathValue("eventID"), "status": "queued"})
}

type metricsResponse struct {
	UptimeSeconds float64          `json:"uptime_seconds"`
	Requests      int64            `json:"requests"`
	InFlight      int64            `json:"in_flight"`
	Status        map[string]int64 `json:"responses_by_class"`
	Scheduler     SchedulerStats   `json:"scheduler"`
	Store         StoreStats       `json:"store"`
	Webhooks      WebhookStats     `json:"webhooks"`
	Goroutines    int              `json:"goroutines"`
	HeapAllocMB   float64          `json:"heap_alloc_mb"`
	Config        map[string]any   `json:"config"`
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	status := map[string]int64{}
	for class := 1; class < len(s.byStatus); class++ {
		if n := s.byStatus[class].Load(); n > 0 {
			status[strconv.Itoa(class)+"xx"] = n
		}
	}
	writeJSON(w, http.StatusOK, metricsResponse{
		UptimeSeconds: time.Since(s.started).Seconds(), Requests: s.requests.Load(), InFlight: s.inFlight.Load(),
		Status: status, Scheduler: s.scheduler.Stats(), Store: s.store.Stats(), Webhooks: s.webhooks.Stats(),
		Goroutines: runtime.NumGoroutine(), HeapAllocMB: float64(mem.HeapAlloc) / (1 << 20),
		Config: map[string]any{
			"seed": s.cfg.Seed, "time_scale": s.cfg.TimeScale, "api_latency": s.cfg.APILatency,
			"auto_capture": s.cfg.AutoCapture, "enforce_receipt_unique": s.cfg.EnforceReceiptUnique,
			"duplicate_pays": s.cfg.DuplicatePays,
		},
	})
}

func decodeJSON(r *http.Request, out any) error {
	body := http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(body)
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func respond[T any](w http.ResponseWriter, value T, apiErr *APIError) {
	if apiErr != nil {
		writeError(w, apiErr)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

type errorBody struct {
	Error struct {
		Code        string         `json:"code"`
		Description string         `json:"description"`
		Source      string         `json:"source"`
		Step        string         `json:"step"`
		Reason      string         `json:"reason"`
		Metadata    map[string]any `json:"metadata"`
		Field       *string        `json:"field,omitempty"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, apiErr *APIError) {
	var body errorBody
	body.Error.Code, body.Error.Description = apiErr.Code, apiErr.Description
	body.Error.Source, body.Error.Step, body.Error.Reason = apiErr.Source, apiErr.Step, apiErr.Reason
	body.Error.Metadata = map[string]any{}
	if apiErr.Field != "" {
		field := apiErr.Field
		body.Error.Field = &field
	}
	writeJSON(w, apiErr.Status, body)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

// Unwrap lets http.ResponseController reach the real writer (needed to drop a
// connection for the "lost response" fault).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Run serves until the context ends.
func (s *Server) Run(ctx context.Context, addr string) error {
	server := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
