package controllers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateCheckoutRejectsOversizedBody(t *testing.T) {
	item := `{"product_id":"00000000-0000-4000-8000-000000000001","quantity":1}`
	body := `{"items":[` + strings.Repeat(item+",", 199) + item + `]}`
	if len(body) <= maxCheckoutRequestBody {
		t.Fatalf("test body length = %d, want more than %d", len(body), maxCheckoutRequestBody)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/checkouts", strings.NewReader(body))
	response := httptest.NewRecorder()
	new(CheckoutController).CreateCheckout(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestGetCheckoutRejectsInvalidOrderID(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/checkouts/not-a-uuid", nil)
	request.SetPathValue("orderID", "not-a-uuid")
	response := httptest.NewRecorder()
	new(CheckoutController).GetCheckout(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}
