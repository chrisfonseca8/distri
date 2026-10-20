package clients

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"
)

func TestListReservedBuildsFilteredRequest(t *testing.T) {
	cutoff := time.Date(2026, 10, 20, 12, 34, 56, 123, time.UTC)
	cursor := &ReservationCursor{CreatedAt: cutoff.Add(-2 * time.Minute), ID: uuid.New()}
	want := ReservationSummary{
		ReservationID: uuid.New(),
		OrderID:       uuid.New(),
		Status:        "RESERVED",
		CreatedAt:     cutoff.Add(-time.Minute),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/internal/v1/reservations" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("status") != "RESERVED" {
			t.Errorf("status = %q", r.URL.Query().Get("status"))
		}
		if r.URL.Query().Get("older_than") != cutoff.Format(time.RFC3339Nano) {
			t.Errorf("older_than = %q", r.URL.Query().Get("older_than"))
		}
		if r.URL.Query().Get("limit") != "25" {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		if r.URL.Query().Get("after_created_at") != cursor.CreatedAt.Format(time.RFC3339Nano) {
			t.Errorf("after_created_at = %q", r.URL.Query().Get("after_created_at"))
		}
		if r.URL.Query().Get("after_id") != cursor.ID.String() {
			t.Errorf("after_id = %q", r.URL.Query().Get("after_id"))
		}
		_ = json.NewEncoder(w).Encode([]ReservationSummary{want})
	}))
	defer server.Close()

	client := &HTTPInventoryClient{baseURL: server.URL, httpClient: server.Client()}
	got, err := client.ListReserved(context.Background(), cutoff, cursor, 25)
	if err != nil {
		t.Fatalf("list reserved: %v", err)
	}
	if len(got) != 1 || got[0].ReservationID != want.ReservationID || got[0].OrderID != want.OrderID {
		t.Fatalf("reservations = %+v, want %+v", got, want)
	}
}
