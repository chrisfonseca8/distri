package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/inventory-service/pkg/services"
)

type ReservationController struct {
	service *services.ReservationService
}

// ListReservations handles GET /internal/v1/reservations and replies with RESERVED reservations older than a given time, one page at a time.
func (c *ReservationController) ListReservations(w http.ResponseWriter, r *http.Request) {
	if strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))) != "RESERVED" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be RESERVED"})
		return
	}

	olderThan, err := time.Parse(time.RFC3339Nano, r.URL.Query().Get("older_than"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "older_than must be an RFC3339 timestamp"})
		return
	}
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be an integer"})
		return
	}

	var cursor *services.ReservationCursor
	afterCreatedAtValue := r.URL.Query().Get("after_created_at")
	afterIDValue := r.URL.Query().Get("after_id")
	if (afterCreatedAtValue == "") != (afterIDValue == "") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "after_created_at and after_id must be provided together"})
		return
	}
	if afterCreatedAtValue != "" {
		afterCreatedAt, timeErr := time.Parse(time.RFC3339Nano, afterCreatedAtValue)
		afterID, idErr := uuid.Parse(afterIDValue)
		if timeErr != nil || idErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid reservation cursor"})
			return
		}
		cursor = &services.ReservationCursor{CreatedAt: afterCreatedAt, ID: afterID}
	}

	reservations, err := c.service.ListReservedReservations(r.Context(), olderThan, cursor, limit)
	if err != nil {
		writeReservationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reservations)
}

// NewReservationController creates the reservation HTTP controller.
func NewReservationController(service *services.ReservationService) *ReservationController {
	return &ReservationController{service: service}
}

// CreateReservation handles POST /internal/v1/reservations and reserves stock for an order.
func (c *ReservationController) CreateReservation(w http.ResponseWriter, r *http.Request) {
	var input services.CreateReservationInput
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	result, err := c.service.CreateReservation(r.Context(), input)
	if err != nil {
		writeReservationError(w, err)
		return
	}

	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}

// ConfirmReservation handles the confirm endpoint and turns an order's reserved stock into sold stock.
func (c *ReservationController) ConfirmReservation(w http.ResponseWriter, r *http.Request) {
	orderID, err := uuid.Parse(r.PathValue("orderID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid order ID"})
		return
	}

	result, err := c.service.ConfirmReservation(r.Context(), orderID)
	if err != nil {
		writeReservationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ReleaseReservation handles the release endpoint and returns an order's reserved stock to the shelf.
func (c *ReservationController) ReleaseReservation(w http.ResponseWriter, r *http.Request) {
	orderID, err := uuid.Parse(r.PathValue("orderID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid order ID"})
		return
	}

	result, err := c.service.ReleaseReservation(r.Context(), orderID)
	if err != nil {
		writeReservationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// writeReservationError turns a reservation error into the matching HTTP status and message.
func writeReservationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrInvalidReservationInput):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrProductNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrReservationNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrReservationConflict),
		errors.Is(err, services.ErrInsufficientStock),
		errors.Is(err, services.ErrReservationStateConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not process reservation"})
	}
}
