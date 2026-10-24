package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	SignatureHeader = "X-Razorpay-Signature"
	EventIDHeader   = "x-razorpay-event-id"

	StatusPending = "PENDING"
	StatusIgnored = "IGNORED"

	// storeTimeout keeps durable acceptance well inside Razorpay's five-second
	// acknowledgement window.
	storeTimeout = 3 * time.Second
)

// supportedEvents are the event types the payment flow acts on. Any other
// authenticated event is acknowledged and recorded as IGNORED.
var supportedEvents = map[string]bool{
	"payment.authorized": true,
	"payment.captured":   true,
	"payment.failed":     true,
	"order.paid":         true,
	"refund.created":     true,
	"refund.processed":   true,
	"refund.failed":      true,
}

// NewEvent is what the handler asks the inbox to record.
type NewEvent struct {
	EventID   string
	EventType string
	Status    string
	Body      []byte
	// GatewayOrderID is read from the verified payload (empty when absent) so a
	// parked event can be found again once the order mapping exists.
	GatewayOrderID string
}

// EventStore is the durable inbox. Insert reports whether the event was new: a
// repeated event id must insert nothing and return (false, nil).
type EventStore interface {
	Insert(ctx context.Context, event NewEvent) (inserted bool, err error)
}

// Handler is the POST webhook endpoint.
type Handler struct {
	secret  []byte
	maxBody int64
	store   EventStore
}

func NewHandler(secret string, maxBody int64, store EventStore) (*Handler, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("webhook secret is required")
	}
	if maxBody <= 0 {
		return nil, errors.New("webhook body limit must be greater than zero")
	}
	if store == nil {
		return nil, errors.New("webhook event store is required")
	}
	return &Handler{secret: []byte(secret), maxBody: maxBody, store: store}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The raw body is read once, bounded, and verified before anything is parsed.
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.maxBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "body too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read body"})
		return
	}

	if !Verify(h.secret, raw, r.Header.Get(SignatureHeader)) {
		slog.Warn("webhook rejected: invalid signature", "remote", r.RemoteAddr, "bytes", len(raw))
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid signature"})
		return
	}

	eventID := strings.TrimSpace(r.Header.Get(EventIDHeader))
	if eventID == "" {
		// Authenticated but impossible to deduplicate: refuse rather than guess.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing " + EventIDHeader})
		return
	}

	var envelope struct {
		Event   string `json:"event"`
		Payload struct {
			Payment struct {
				Entity struct {
					OrderID string `json:"order_id"`
				} `json:"entity"`
			} `json:"payment"`
		} `json:"payload"`
	}
	eventType, status, gatewayOrderID := "", StatusIgnored, ""
	if err := json.Unmarshal(raw, &envelope); err == nil {
		eventType = envelope.Event
		gatewayOrderID = envelope.Payload.Payment.Entity.OrderID
		if supportedEvents[eventType] {
			status = StatusPending
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	inserted, err := h.store.Insert(ctx, NewEvent{EventID: eventID, EventType: eventType, Status: status, Body: raw, GatewayOrderID: gatewayOrderID})
	if err != nil {
		// Not durably accepted: a non-2xx makes Razorpay retry.
		slog.Error("webhook not recorded", "event_id", eventID, "event", eventType, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not record event"})
		return
	}

	result := "accepted"
	switch {
	case !inserted:
		result = "duplicate"
	case status == StatusIgnored:
		result = "ignored"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": result})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
