package controllers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/chrisfonseca08/distri/inventory-service/pkg/services"
)

type ReservationController struct {
	service *services.ReservationService
}

func NewReservationController(service *services.ReservationService) *ReservationController {
	return &ReservationController{service: service}
}

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

func writeReservationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrInvalidReservationInput):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrProductNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrReservationConflict),
		errors.Is(err, services.ErrInsufficientStock):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create reservation"})
	}
}
