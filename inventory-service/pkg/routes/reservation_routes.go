package routes

import (
	"net/http"

	"github.com/chrisfonseca08/distri/inventory-service/pkg/controllers"
)

// RegisterReservationRoutes adds the internal reservation endpoints to mux.
func RegisterReservationRoutes(mux *http.ServeMux, controller *controllers.ReservationController) {
	mux.HandleFunc("GET /internal/v1/reservations", controller.ListReservations)
	mux.HandleFunc("POST /internal/v1/reservations", controller.CreateReservation)
	mux.HandleFunc("POST /internal/v1/reservations/{orderID}/confirm", controller.ConfirmReservation)
	mux.HandleFunc("POST /internal/v1/reservations/{orderID}/release", controller.ReleaseReservation)
}
