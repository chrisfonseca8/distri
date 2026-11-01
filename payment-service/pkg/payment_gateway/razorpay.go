package paymentgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chrisfonseca08/distri/payment-service/pkg/businessmetrics"
	"github.com/chrisfonseca08/distri/payment-service/pkg/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"uuid"
)

const (
	defaultRazorpayBaseURL  = "https://api.razorpay.com"
	defaultRazorpayCurrency = "INR"
	testKeyPrefix           = "rzp_test_"
	maxResponseBytes        = 1 << 20
)

// RazorpayConfig configures the Razorpay adapter. Credentials are Razorpay API
// keys: the key id (rzp_test_...) and the key secret, sent as HTTP Basic auth.
type RazorpayConfig struct {
	KeyID     string
	KeySecret string
	BaseURL   string
	Currency  string
	// AllowLive permits non-test keys. The project runs in Test Mode only.
	AllowLive  bool
	HTTPClient *http.Client
}

// Razorpay is a Gateway backed by the Razorpay Orders API.
//
// Razorpay has no idempotency header for orders. The checkout's order ID is
// sent as the order's receipt so the order can be found again by lookup. The
// docs say receipts must be unique and a repeat is rejected, but the real Test
// Mode API was observed to accept duplicates, so uniqueness cannot be relied on:
// callers must create at most one order per checkout (payment-service never
// retries CreateCharge after the call started) and resolve ambiguity by lookup.
// The duplicate-receipt branch in CreateCharge only runs if Razorpay does reject.
type Razorpay struct {
	baseURL   string
	keyID     string
	keySecret string
	currency  string
	client    *http.Client
}

var (
	_ Gateway        = (*Razorpay)(nil)
	_ OrderLookup    = (*Razorpay)(nil)
	_ Refunder       = (*Razorpay)(nil)
	_ RefundLookup   = (*Razorpay)(nil)
	_ PaymentFetcher = (*Razorpay)(nil)
)

// NewRazorpay validates the configuration and creates the adapter.
func NewRazorpay(config RazorpayConfig) (*Razorpay, error) {
	keyID := strings.TrimSpace(config.KeyID)
	keySecret := strings.TrimSpace(config.KeySecret)
	if keyID == "" {
		return nil, errors.New("RAZORPAY_KEY_ID is not set")
	}
	if keySecret == "" {
		return nil, errors.New("RAZORPAY_KEY_SECRET is not set")
	}
	if !config.AllowLive && !strings.HasPrefix(keyID, testKeyPrefix) {
		return nil, fmt.Errorf(
			"RAZORPAY_KEY_ID must be a Test Mode key (%s...); set RAZORPAY_ALLOW_LIVE=true to override",
			testKeyPrefix,
		)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultRazorpayBaseURL
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("RAZORPAY_BASE_URL is not a valid URL: %w", err)
	}
	currency := strings.ToUpper(strings.TrimSpace(config.Currency))
	if currency == "" {
		currency = defaultRazorpayCurrency
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &Razorpay{
		baseURL:   baseURL,
		keyID:     keyID,
		keySecret: keySecret,
		currency:  currency,
		client:    client,
	}, nil
}

// NewRazorpayFromEnv reads RAZORPAY_KEY_ID, RAZORPAY_KEY_SECRET and the optional
// RAZORPAY_BASE_URL, RAZORPAY_CURRENCY and RAZORPAY_ALLOW_LIVE.
func NewRazorpayFromEnv() (*Razorpay, error) {
	allowLive := strings.EqualFold(strings.TrimSpace(os.Getenv("RAZORPAY_ALLOW_LIVE")), "true")
	return NewRazorpay(RazorpayConfig{
		KeyID:     os.Getenv("RAZORPAY_KEY_ID"),
		KeySecret: os.Getenv("RAZORPAY_KEY_SECRET"),
		BaseURL:   os.Getenv("RAZORPAY_BASE_URL"),
		Currency:  os.Getenv("RAZORPAY_CURRENCY"),
		AllowLive: allowLive,
	})
}

type razorpayError struct {
	Error struct {
		Code        string `json:"code"`
		Description string `json:"description"`
		Reason      string `json:"reason"`
	} `json:"error"`
}

type razorpayOrder struct {
	ID      string `json:"id"`
	Amount  int64  `json:"amount"`
	Receipt string `json:"receipt"`
	Status  string `json:"status"`
}

type razorpayPayment struct {
	ID        string `json:"id"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Status    string `json:"status"`
	ErrorCode string `json:"error_code"`
}

// CreateCharge creates the Razorpay order for the checkout. A successful create
// is not a payment: it returns *ErrAwaitingPayment with the Razorpay order id.
//
// A confirmed rejection returns *ErrRejected. Everything that leaves the outcome
// unknown (timeout, network error, 5xx, 429, bad credentials) returns a plain
// error, which callers must treat as ambiguous and resolve by lookup.
func (r *Razorpay) CreateCharge(
	ctx context.Context,
	orderID uuid.UUID,
	amountPaise int64,
) (string, error) {
	receipt := orderID.String()
	body := map[string]any{
		"amount":   amountPaise,
		"currency": r.currency,
		"receipt":  receipt,
		"notes":    map[string]string{"order_id": receipt},
	}
	var order razorpayOrder
	status, apiErr, err := r.do(ctx, http.MethodPost, "/v1/orders", nil, body, &order)
	if err != nil {
		return "", fmt.Errorf("create razorpay order: %w", err)
	}

	switch {
	case status >= 200 && status < 300:
		if order.ID == "" {
			return "", errors.New("create razorpay order: response has no order id")
		}
		if order.Amount != amountPaise || order.Receipt != receipt {
			return "", fmt.Errorf(
				"create razorpay order: response does not match the request (amount %d, receipt %q)",
				order.Amount, order.Receipt,
			)
		}
		return "", &ErrAwaitingPayment{Ref: order.ID}
	case isDuplicateReceipt(status, apiErr):
		state, lookupErr := r.LookupOrder(ctx, orderID)
		if lookupErr != nil {
			return "", fmt.Errorf("create razorpay order: duplicate receipt, lookup failed: %w", lookupErr)
		}
		if state.AmountPaise != amountPaise {
			return "", &ErrRejected{
				Code:        "AMOUNT_MISMATCH",
				Description: fmt.Sprintf("existing order has amount %d, want %d", state.AmountPaise, amountPaise),
			}
		}
		return "", &ErrAwaitingPayment{Ref: state.GatewayOrderID}
	default:
		return "", classifyFailure("create razorpay order", status, apiErr)
	}
}

// LookupOrder finds the checkout's order by receipt and reports whether a
// payment on it has been captured. It returns ErrOrderNotFound when Razorpay has
// no such order. An authorized-but-uncaptured payment counts as not paid.
//
// Two behaviours of the real Test Mode API shape this (observed 2026-10-23, see
// memory.md): the receipt filter is eventually consistent, so an order created
// moments ago may not be listed yet (ErrOrderNotFound then means "not yet", and
// callers must wait out the payment window before concluding there is none); and
// receipt uniqueness is not enforced, so several orders can share a receipt. In
// that case every order is checked for a capture, and without one the oldest
// order is reported.
func (r *Razorpay) LookupOrder(ctx context.Context, orderID uuid.UUID) (OrderState, error) {
	query := url.Values{}
	query.Set("receipt", orderID.String())
	query.Set("count", "10")
	var orders struct {
		Items []razorpayOrder `json:"items"`
	}
	status, apiErr, err := r.do(ctx, http.MethodGet, "/v1/orders", query, nil, &orders)
	if err != nil {
		return OrderState{}, fmt.Errorf("find razorpay order: %w", err)
	}
	if status < 200 || status >= 300 {
		return OrderState{}, classifyFailure("find razorpay order", status, apiErr)
	}
	if len(orders.Items) == 0 {
		return OrderState{}, ErrOrderNotFound
	}

	// The list is newest first; the oldest order is the one our create produced.
	oldest := orders.Items[len(orders.Items)-1]
	state := OrderState{GatewayOrderID: oldest.ID, AmountPaise: oldest.Amount}
	// Oldest first, so the capture on the order our create produced is the primary
	// one and any capture elsewhere is reported as an extra.
	for i := len(orders.Items) - 1; i >= 0; i-- {
		order := orders.Items[i]
		var payments struct {
			Items []razorpayPayment `json:"items"`
		}
		status, apiErr, err = r.do(ctx, http.MethodGet, "/v1/orders/"+url.PathEscape(order.ID)+"/payments", nil, nil, &payments)
		if err != nil {
			return OrderState{}, fmt.Errorf("list razorpay payments: %w", err)
		}
		if status < 200 || status >= 300 {
			return OrderState{}, classifyFailure("list razorpay payments", status, apiErr)
		}
		for _, payment := range payments.Items {
			switch payment.Status {
			case "captured":
				if state.CapturedPaymentID == "" {
					state.GatewayOrderID = order.ID
					state.AmountPaise = order.Amount
					state.CapturedPaymentID = payment.ID
					state.CapturedAmountPaise = payment.Amount
					state.CapturedCurrency = payment.Currency
				} else {
					state.ExtraCaptured = append(state.ExtraCaptured, CapturedPayment{
						PaymentID: payment.ID, AmountPaise: payment.Amount, Currency: payment.Currency,
					})
				}
			case "failed":
				if state.LastFailureCode == "" {
					state.LastFailureCode = payment.ErrorCode
				}
			}
		}
	}
	return state, nil
}

type razorpayRefund struct {
	ID        string          `json:"id"`
	Status    string          `json:"status"`
	PaymentID string          `json:"payment_id"`
	Amount    int64           `json:"amount"`
	Receipt   *string         `json:"receipt"`
	Notes     json.RawMessage `json:"notes"`
}

// toResult converts a provider refund. Razorpay returns "notes": [] when empty,
// so notes are decoded leniently.
func (rf razorpayRefund) toResult() RefundResult {
	result := RefundResult{ID: rf.ID, Status: rf.Status, PaymentID: rf.PaymentID, AmountPaise: rf.Amount}
	if rf.Receipt != nil {
		result.Receipt = *rf.Receipt
	}
	var notes map[string]any
	if json.Unmarshal(rf.Notes, &notes) == nil && len(notes) > 0 {
		result.Notes = make(map[string]string, len(notes))
		for key, value := range notes {
			result.Notes[key] = fmt.Sprint(value)
		}
	}
	return result
}

// Refund submits one refund. It sends Razorpay's documented X-Refund-Idempotency
// header when the request carries a key. What that guarantees beyond "retry with the
// same key and body without risk of a double refund" is not documented (retention,
// replay response shape), so callers reconcile by lookup rather than assume it.
//
// Outcomes: nil error = the provider accepted the request (inspect Status).
// *ErrRejected = definitive refusal, no refund was created. ErrFullyRefunded and
// *ErrRefundConflict = a refund may already exist: reconcile. Any other error
// (timeout, reset, 5xx, 429) is AMBIGUOUS: the refund may or may not exist.
func (r *Razorpay) Refund(ctx context.Context, request RefundRequest) (RefundResult, error) {
	if strings.TrimSpace(request.PaymentID) == "" {
		return RefundResult{}, errors.New("refund razorpay payment: payment id is required")
	}
	body := map[string]any{}
	if request.AmountPaise > 0 {
		body["amount"] = request.AmountPaise
	}
	if request.Receipt != "" {
		body["receipt"] = request.Receipt
	}
	if len(request.Notes) > 0 {
		body["notes"] = request.Notes
	}
	var headers map[string]string
	if request.IdempotencyKey != "" {
		headers = map[string]string{"X-Refund-Idempotency": request.IdempotencyKey}
	}
	var refund razorpayRefund
	status, apiErr, err := r.doWith(ctx, http.MethodPost, "/v1/payments/"+url.PathEscape(request.PaymentID)+"/refund", nil, body, &refund, headers)
	if err != nil {
		return RefundResult{}, fmt.Errorf("refund razorpay payment: %w", err)
	}
	switch {
	case status >= 200 && status < 300:
		return refund.toResult(), nil
	case status == http.StatusConflict:
		description := ""
		if apiErr != nil {
			description = apiErr.Error.Description
		}
		return RefundResult{}, &ErrRefundConflict{Description: description}
	case apiErr != nil && strings.Contains(strings.ToLower(apiErr.Error.Description), "fully refunded"):
		return RefundResult{}, ErrFullyRefunded
	default:
		return RefundResult{}, classifyFailure("refund razorpay payment", status, apiErr)
	}
}

// FetchRefund reads one refund (GET /v1/refunds/{id}, documented). An unknown id is
// HTTP 400 "The id provided does not exist" -> ErrRefundNotFound.
func (r *Razorpay) FetchRefund(ctx context.Context, refundID string) (RefundResult, error) {
	var refund razorpayRefund
	status, apiErr, err := r.do(ctx, http.MethodGet, "/v1/refunds/"+url.PathEscape(refundID), nil, nil, &refund)
	if err != nil {
		return RefundResult{}, fmt.Errorf("fetch razorpay refund: %w", err)
	}
	if status >= 200 && status < 300 {
		return refund.toResult(), nil
	}
	if apiErr != nil && strings.Contains(strings.ToLower(apiErr.Error.Description), "does not exist") {
		return RefundResult{}, ErrRefundNotFound
	}
	return RefundResult{}, classifyFailure("fetch razorpay refund", status, apiErr)
}

// ListPaymentRefunds lists the refunds of one payment (GET /v1/payments/{id}/refunds,
// listed in Razorpay's refund index; its pagination parameters are not documented, so
// count is requested but a full page is not assumed to be everything).
func (r *Razorpay) ListPaymentRefunds(ctx context.Context, paymentID string) ([]RefundResult, error) {
	query := url.Values{}
	query.Set("count", "100")
	var list struct {
		Items []razorpayRefund `json:"items"`
	}
	status, apiErr, err := r.do(ctx, http.MethodGet, "/v1/payments/"+url.PathEscape(paymentID)+"/refunds", query, nil, &list)
	if err != nil {
		return nil, fmt.Errorf("list razorpay refunds: %w", err)
	}
	if status >= 200 && status < 300 {
		results := make([]RefundResult, 0, len(list.Items))
		for _, item := range list.Items {
			results = append(results, item.toResult())
		}
		return results, nil
	}
	if apiErr != nil && strings.Contains(strings.ToLower(apiErr.Error.Description), "does not exist") {
		return nil, ErrPaymentNotFound
	}
	return nil, classifyFailure("list razorpay refunds", status, apiErr)
}

// FetchPayment reads one payment (GET /v1/payments/{id}, documented), including
// amount_refunded: how much of it has been refunded by anyone, which is how
// refunds we did not create are noticed.
func (r *Razorpay) FetchPayment(ctx context.Context, paymentID string) (PaymentInfo, error) {
	var payment struct {
		ID             string `json:"id"`
		OrderID        string `json:"order_id"`
		Status         string `json:"status"`
		Currency       string `json:"currency"`
		Amount         int64  `json:"amount"`
		AmountRefunded int64  `json:"amount_refunded"`
	}
	status, apiErr, err := r.do(ctx, http.MethodGet, "/v1/payments/"+url.PathEscape(paymentID), nil, nil, &payment)
	if err != nil {
		return PaymentInfo{}, fmt.Errorf("fetch razorpay payment: %w", err)
	}
	if status >= 200 && status < 300 {
		return PaymentInfo{
			ID: payment.ID, OrderID: payment.OrderID, Status: payment.Status, Currency: payment.Currency,
			AmountPaise: payment.Amount, AmountRefundedPaise: payment.AmountRefunded,
		}, nil
	}
	if apiErr != nil && strings.Contains(strings.ToLower(apiErr.Error.Description), "does not exist") {
		return PaymentInfo{}, ErrPaymentNotFound
	}
	return PaymentInfo{}, classifyFailure("fetch razorpay payment", status, apiErr)
}

// do sends one request. A non-nil error means the outcome is unknown (the
// request may or may not have been processed). Otherwise status is the HTTP
// status and, for an error status, apiErr holds Razorpay's error body.
func (r *Razorpay) do(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	body any,
	out any,
) (int, *razorpayError, error) {
	return r.doWith(ctx, method, path, query, body, out, nil)
}

// doWith is do with extra request headers.
func (r *Razorpay) doWith(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	body any,
	out any,
	headers map[string]string,
) (statusCode int, failure *razorpayError, err error) {
	operation := gatewayOperation(method, path)
	ctx, span := observability.StartSpan(ctx, "gateway "+operation, trace.SpanKindClient, attribute.String("peer.service", "razorpay"))
	started := time.Now()
	defer func() {
		result := "ok"
		var notSent *ErrNotSent
		switch {
		case errors.As(err, &notSent):
			result = "not_sent"
		case err != nil:
			result = "ambiguous"
		case failure != nil && statusCode >= 500, statusCode == http.StatusTooManyRequests:
			result = "ambiguous"
		case failure != nil:
			result = "rejected"
		}
		businessmetrics.GatewayCalls.WithLabelValues(operation, result).Inc()
		businessmetrics.GatewayDuration.WithLabelValues(operation).Observe(time.Since(started).Seconds())
		span.SetAttributes(attribute.Int("http.response.status_code", statusCode), attribute.String("gateway.result", result))
		if result == "ambiguous" || result == "not_sent" {
			observability.EndSpan(span, errors.New("gateway call "+result))
		} else {
			span.End()
		}
	}()
	target := r.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	observability.InjectHTTP(ctx, request.Header)
	request.SetBasicAuth(r.keyID, r.keySecret)
	request.Header.Set("Accept", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := r.client.Do(request)
	if err != nil {
		var opError *net.OpError
		if errors.As(err, &opError) && opError.Op == "dial" {
			// The connection was never established, so the request cannot have reached
			// the provider.
			return 0, nil, &ErrNotSent{Err: err}
		}
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return response.StatusCode, nil, fmt.Errorf("read response: %w", err)
	}

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				return response.StatusCode, nil, fmt.Errorf("decode response: %w", err)
			}
		}
		return response.StatusCode, nil, nil
	}
	var apiErr razorpayError
	_ = json.Unmarshal(raw, &apiErr)
	return response.StatusCode, &apiErr, nil
}

// gatewayOperation names a gateway call for metrics: method plus the path with ids
// removed, so the label stays bounded (for example "POST /payments/refund").
func gatewayOperation(method, path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	parts := []string{}
	for index, segment := range segments {
		// Collection names are letters only; ids contain digits or underscores.
		if index%2 == 0 && !strings.ContainsAny(segment, "0123456789_") {
			parts = append(parts, segment)
		}
	}
	return method + " /" + strings.Join(parts, "/")
}

// classifyFailure maps an error response to a typed rejection or an ambiguous
// error. Razorpay answers even bad credentials with HTTP 400, so credentials
// problems are recognised from the description and treated as ambiguous: a
// broken key must park payments for review, not cancel customers' orders.
func classifyFailure(action string, status int, apiErr *razorpayError) error {
	description, code := "", ""
	if apiErr != nil {
		description, code = apiErr.Error.Description, apiErr.Error.Code
	}
	switch {
	case status == http.StatusUnauthorized,
		status == http.StatusForbidden,
		strings.Contains(strings.ToLower(description), "authentication failed"):
		return fmt.Errorf("%s: credentials rejected (HTTP %d): %s", action, status, description)
	case status == http.StatusTooManyRequests, status >= 500:
		return fmt.Errorf("%s: HTTP %d: %s", action, status, description)
	case status >= 400:
		return &ErrRejected{Code: code, Description: description}
	default:
		return fmt.Errorf("%s: unexpected HTTP %d", action, status)
	}
}

// isDuplicateReceipt reports whether Razorpay rejected the request because the same receipt was already used.
func isDuplicateReceipt(status int, apiErr *razorpayError) bool {
	if status != http.StatusBadRequest || apiErr == nil {
		return false
	}
	description := strings.ToLower(apiErr.Error.Description)
	return strings.Contains(description, "duplicate request") ||
		strings.Contains(description, "already been processed") ||
		(strings.Contains(description, "receipt") && strings.Contains(description, "unique"))
}
