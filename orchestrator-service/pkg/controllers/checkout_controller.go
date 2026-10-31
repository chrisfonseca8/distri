package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/services/checkout"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/statusstream"
)

const maxCheckoutRequestBody = 8 << 10

const (
	sseHeartbeat    = 15 * time.Second
	sseWriteTimeout = 5 * time.Second
)

type CheckoutController struct {
	service checkoutService
	hub     *statusstream.Hub
}

type checkoutService interface {
	CreateCheckout(context.Context, checkout.CreateCheckoutInput) (checkout.Response, error)
	GetCheckout(context.Context, uuid.UUID) (checkout.GetCheckoutResult, error)
	GetCheckoutStatus(context.Context, uuid.UUID) (checkout.CheckoutStatusResult, error)
}

// NewCheckoutController creates the checkout HTTP controller.
func NewCheckoutController(service checkoutService, hubs ...*statusstream.Hub) *CheckoutController {
	controller := &CheckoutController{service: service}
	if len(hubs) > 0 {
		controller.hub = hubs[0]
	}
	return controller
}

// CreateCheckout handles POST /v1/checkouts and replies with the stored result of the checkout.
func (c *CheckoutController) CreateCheckout(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCheckoutRequestBody)
	input, err := decodeCheckoutInput(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	response, err := c.service.CreateCheckout(r.Context(), input)
	if err != nil {
		writeCheckoutError(w, err)
		return
	}
	writeRawJSON(w, response.StatusCode, response.Body)
}

// GetCheckout handles GET /v1/checkouts/{orderID} and replies with the checkout's current state.
func (c *CheckoutController) GetCheckout(w http.ResponseWriter, r *http.Request) {
	orderID, err := uuid.Parse(r.PathValue("orderID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid order ID"})
		return
	}

	result, err := c.service.GetCheckout(r.Context(), orderID)
	if err != nil {
		switch {
		case errors.Is(err, checkout.ErrCheckoutNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		default:
			slog.Error("get checkout", "order_id", orderID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not get checkout"})
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type checkoutStatusEvent struct {
	OrderID     uuid.UUID `json:"order_id"`
	Status      string    `json:"status"`
	CurrentStep string    `json:"current_step"`
	Version     int64     `json:"version"`
	UpdatedAt   time.Time `json:"updated_at"`
	TotalPaise  int64     `json:"total_paise"`
	Currency    string    `json:"currency"`
}

// StreamCheckout sends an authoritative snapshot followed by committed saga changes.
// The snapshot after subscribing closes the commit-before-subscribe race: either the
// snapshot sees the change or the hub notification causes another read.
func (c *CheckoutController) StreamCheckout(w http.ResponseWriter, r *http.Request) {
	if c.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "checkout event stream is unavailable"})
		return
	}
	orderID, err := uuid.Parse(r.PathValue("orderID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid order ID"})
		return
	}
	changes, cancel, err := c.hub.Subscribe(orderID.String())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "checkout event stream is at capacity"})
		return
	}
	defer cancel()

	result, err := c.service.GetCheckoutStatus(r.Context(), orderID)
	if err != nil {
		if errors.Is(err, checkout.ErrCheckoutNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		} else {
			slog.Error("stream checkout initial snapshot", "order_id", orderID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not get checkout"})
		}
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	response := http.NewResponseController(w)
	lastVersion := int64(0)
	writeSnapshot := func(result checkout.CheckoutStatusResult) error {
		if result.Version <= lastVersion {
			return nil
		}
		event := checkoutStatusEvent{
			OrderID: result.OrderID, Status: result.Status, CurrentStep: result.CurrentStep,
			Version: result.Version, UpdatedAt: result.UpdatedAt, TotalPaise: result.TotalPaise, Currency: result.Currency,
		}
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		_ = response.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
		if _, err := fmt.Fprintf(w, "id: %d\nevent: checkout.status\ndata: %s\n\n", result.Version, data); err != nil {
			return err
		}
		if err := response.Flush(); err != nil {
			return err
		}
		lastVersion = result.Version
		return nil
	}
	if err := writeSnapshot(result); err != nil || isTerminalCheckout(result.Status) {
		return
	}

	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-changes:
			result, err = c.service.GetCheckoutStatus(r.Context(), orderID)
			if err != nil {
				slog.Error("stream checkout snapshot", "order_id", orderID, "error", err)
				return
			}
			if err := writeSnapshot(result); err != nil || isTerminalCheckout(result.Status) {
				return
			}
		case <-heartbeat.C:
			_ = response.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if err := response.Flush(); err != nil {
				return
			}
		}
	}
}

func isTerminalCheckout(status string) bool {
	return status == "COMPLETED" || status == "COMPENSATED"
}

// decodeCheckoutInput reads the Idempotency-Key header and JSON body, rejecting unknown fields.
func decodeCheckoutInput(r *http.Request) (checkout.CreateCheckoutInput, error) {
	input := checkout.CreateCheckoutInput{
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return checkout.CreateCheckoutInput{}, err
	}
	return input, nil
}

// writeCheckoutError turns a checkout error into the matching HTTP status and message.
func writeCheckoutError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, checkout.ErrInvalidCheckoutInput):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, checkout.ErrIdempotencyKeyConflict):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	case errors.Is(err, checkout.ErrCheckoutInventoryUnavailable):
		slog.Warn("checkout inventory unavailable", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "checkout inventory is unavailable, retry with the same idempotency key"})
	default:
		slog.Error("create checkout", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create checkout"})
	}
}

// writeRawJSON writes an already-encoded JSON body with the given status.
func writeRawJSON(w http.ResponseWriter, statusCode int, body json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

// writeJSON encodes value as JSON and writes it with the given status.
func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
