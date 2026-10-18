package routes

import (
	"net/http"

	"github.com/chrisfonseca08/distri/inventory-service/pkg/controllers"
)

func RegisterReservationRoutes(mux *http.ServeMux, controller *controllers.ReservationController) {
	mux.HandleFunc("POST /internal/v1/reservations", controller.CreateReservation)
}
