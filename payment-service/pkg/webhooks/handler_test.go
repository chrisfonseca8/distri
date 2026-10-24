package webhooks

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const testSecret = "whsec_test"

type fakeStore struct {
	mu    sync.Mutex
	seen  map[string]NewEvent
	err   error
	calls int
}

func newFakeStore() *fakeStore { return &fakeStore{seen: map[string]NewEvent{}} }

func (s *fakeStore) Insert(_ context.Context, event NewEvent) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return false, s.err
	}
	if _, exists := s.seen[event.EventID]; exists {
		return false, nil
	}
	s.seen[event.EventID] = event
	return true, nil
}

func post(handler http.Handler, body, eventID, signature string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/v1/webhooks/razorpay", strings.NewReader(body))
	if eventID != "" {
		request.Header.Set(EventIDHeader, eventID)
	}
	if signature != "" {
		request.Header.Set(SignatureHeader, signature)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func signed(body string) string { return Sign([]byte(testSecret), []byte(body)) }

func newTestHandler(t *testing.T, store EventStore, maxBody int64) *Handler {
	t.Helper()
	handler, err := NewHandler(testSecret, maxBody, store)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

const capturedBody = `{"entity":"event","event":"payment.captured","payload":{"payment":{"entity":{"id":"pay_1","order_id":"order_1"}}}}`

func TestVerifySignature(t *testing.T) {
	body := []byte(capturedBody)
	good := Sign([]byte(testSecret), body)
	tests := []struct {
		name      string
		secret    string
		body      []byte
		signature string
		want      bool
	}{
		{"valid", testSecret, body, good, true},
		{"wrong secret", "other", body, good, false},
		{"body changed by one byte", testSecret, append([]byte(" "), body...), good, false},
		{"empty signature", testSecret, body, "", false},
		{"not hex", testSecret, body, "zz" + good[2:], false},
		{"truncated", testSecret, body, good[:20], false},
		{"empty secret never verifies", "", body, good, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Verify([]byte(test.secret), test.body, test.signature); got != test.want {
				t.Fatalf("Verify = %v, want %v", got, test.want)
			}
		})
	}
}

func TestInvalidSignatureChangesNothing(t *testing.T) {
	store := newFakeStore()
	handler := newTestHandler(t, store, 1<<20)
	for name, signature := range map[string]string{
		"missing":   "",
		"wrong":     Sign([]byte("not-the-secret"), []byte(capturedBody)),
		"for other": signed(`{"event":"payment.captured"}`),
	} {
		recorder := post(handler, capturedBody, "evt_1", signature)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s signature: status = %d, want 401", name, recorder.Code)
		}
	}
	if store.calls != 0 {
		t.Fatalf("the inbox was written %d times for unauthenticated requests", store.calls)
	}
}

func TestAcceptedEventIsRecordedExactly(t *testing.T) {
	store := newFakeStore()
	handler := newTestHandler(t, store, 1<<20)
	recorder := post(handler, capturedBody, "evt_1", signed(capturedBody))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "accepted") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	event := store.seen["evt_1"]
	if event.Status != StatusPending || event.EventType != "payment.captured" || !bytes.Equal(event.Body, []byte(capturedBody)) {
		t.Fatalf("recorded = %+v", event)
	}
}

func TestDuplicateDeliveryIsAcknowledgedWithoutASecondRecord(t *testing.T) {
	store := newFakeStore()
	handler := newTestHandler(t, store, 1<<20)
	post(handler, capturedBody, "evt_1", signed(capturedBody))
	second := post(handler, capturedBody, "evt_1", signed(capturedBody))
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), "duplicate") {
		t.Fatalf("duplicate: status=%d body=%s", second.Code, second.Body.String())
	}
	if len(store.seen) != 1 {
		t.Fatalf("recorded %d events, want 1", len(store.seen))
	}
}

func TestUnsupportedEventsAreAcknowledgedAndIgnoredDeliberately(t *testing.T) {
	store := newFakeStore()
	handler := newTestHandler(t, store, 1<<20)
	body := `{"entity":"event","event":"subscription.charged","payload":{}}`
	recorder := post(handler, body, "evt_9", signed(body))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "ignored") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if store.seen["evt_9"].Status != StatusIgnored {
		t.Fatalf("recorded = %+v, want IGNORED", store.seen["evt_9"])
	}
}

func TestDurableAcceptanceFailureIsRetryable(t *testing.T) {
	store := newFakeStore()
	store.err = errors.New("database is down")
	handler := newTestHandler(t, store, 1<<20)
	recorder := post(handler, capturedBody, "evt_1", signed(capturedBody))
	if recorder.Code < 500 {
		t.Fatalf("status = %d, want a retryable 5xx when the event is not durably stored", recorder.Code)
	}
}

func TestRequestValidation(t *testing.T) {
	store := newFakeStore()
	handler := newTestHandler(t, store, 64)

	if recorder := post(handler, capturedBody, "evt_1", signed(capturedBody)); recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: status = %d, want 413", recorder.Code)
	}
	big := newTestHandler(t, store, 1<<20)
	if recorder := post(big, capturedBody, "", signed(capturedBody)); recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing event id: status = %d, want 400", recorder.Code)
	}
	// A signed body that is not JSON is authentic but unusable: acknowledged and ignored.
	garbage := "not json at all"
	if recorder := post(big, garbage, "evt_g", signed(garbage)); recorder.Code != http.StatusOK || store.seen["evt_g"].Status != StatusIgnored {
		t.Fatalf("signed garbage: status=%d recorded=%+v", recorder.Code, store.seen["evt_g"])
	}
}

func TestNewHandlerRequiresASecret(t *testing.T) {
	if _, err := NewHandler("  ", 1, newFakeStore()); err == nil {
		t.Fatal("NewHandler accepted an empty secret")
	}
}
