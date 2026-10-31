package controllers

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/services/checkout"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/statusstream"
)

// funcStatusService answers GetCheckoutStatus from a function, so a test can script
// versions, errors and delays per call.
type funcStatusService struct {
	sseCheckoutService
	status func(call int) (checkout.CheckoutStatusResult, error)
	mu2    sync.Mutex
	calls2 int
}

func (s *funcStatusService) GetCheckoutStatus(context.Context, uuid.UUID) (checkout.CheckoutStatusResult, error) {
	s.mu2.Lock()
	call := s.calls2
	s.calls2++
	s.mu2.Unlock()
	return s.status(call)
}

func (s *funcStatusService) statusCalls() int {
	s.mu2.Lock()
	defer s.mu2.Unlock()
	return s.calls2
}

func sseServer(t *testing.T, service checkoutService, hub *statusstream.Hub) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/checkouts/{orderID}/events", NewCheckoutController(service, hub).StreamCheckout)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func waitForSubscribers(t *testing.T, hub *statusstream.Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for hub.Subscribers() != want && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if got := hub.Subscribers(); got != want {
		t.Fatalf("hub subscribers = %d, want %d", got, want)
	}
}

func readAll(t *testing.T, url string) (int, string) {
	t.Helper()
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response.StatusCode, string(raw)
}

func eventIDs(body string) []string {
	var ids []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "id: ") {
			ids = append(ids, strings.TrimPrefix(line, "id: "))
		}
	}
	return ids
}

func TestStreamForAnAlreadyTerminalSagaSendsOneEventAndClosesAtOnce(t *testing.T) {
	for _, terminal := range []string{"COMPLETED", "COMPENSATED"} {
		orderID := uuid.New()
		service := &funcStatusService{status: func(int) (checkout.CheckoutStatusResult, error) {
			return checkout.CheckoutStatusResult{OrderID: orderID, Status: terminal, Version: 9, UpdatedAt: time.Now()}, nil
		}}
		hub := statusstream.NewHub(10)
		server := sseServer(t, service, hub)

		code, body := readAll(t, server.URL+"/v1/checkouts/"+orderID.String()+"/events")

		if code != http.StatusOK || len(eventIDs(body)) != 1 || !strings.Contains(body, `"status":"`+terminal+`"`) {
			t.Fatalf("%s: status=%d body=%q, want one terminal event", terminal, code, body)
		}
		waitForSubscribers(t, hub, 0)
		if service.statusCalls() != 1 {
			t.Fatalf("%s: status reads = %d, a terminal snapshot needs exactly one", terminal, service.statusCalls())
		}
	}
}

func TestStreamRejectsBadRequestsWithoutLeavingASubscriber(t *testing.T) {
	missing := uuid.New()
	service := &funcStatusService{status: func(int) (checkout.CheckoutStatusResult, error) {
		return checkout.CheckoutStatusResult{}, checkout.ErrCheckoutNotFound
	}}
	hub := statusstream.NewHub(10)
	server := sseServer(t, service, hub)

	if code, _ := readAll(t, server.URL+"/v1/checkouts/not-a-uuid/events"); code != http.StatusBadRequest {
		t.Fatalf("invalid id: status %d, want 400", code)
	}
	if code, _ := readAll(t, server.URL+"/v1/checkouts/"+missing.String()+"/events"); code != http.StatusNotFound {
		t.Fatalf("unknown saga: status %d, want 404", code)
	}
	waitForSubscribers(t, hub, 0)

	failing := &funcStatusService{status: func(int) (checkout.CheckoutStatusResult, error) {
		return checkout.CheckoutStatusResult{}, errors.New("database down")
	}}
	failingServer := sseServer(t, failing, hub)
	if code, body := readAll(t, failingServer.URL+"/v1/checkouts/"+missing.String()+"/events"); code != http.StatusInternalServerError || strings.Contains(body, "database down") {
		t.Fatalf("snapshot failure: status %d body %q, want 500 without leaking the cause", code, body)
	}
	waitForSubscribers(t, hub, 0)

	unavailable := httptest.NewServer(http.HandlerFunc(NewCheckoutController(service).StreamCheckout))
	defer unavailable.Close()
	if code, _ := readAll(t, unavailable.URL+"/x"); code != http.StatusServiceUnavailable {
		t.Fatalf("no hub configured: status %d, want 503", code)
	}
}

func TestStreamAtSubscriberCapacityAnswers503AndFreesTheSlotWhenAnotherEnds(t *testing.T) {
	orderID := uuid.New()
	running := func(int) (checkout.CheckoutStatusResult, error) {
		return checkout.CheckoutStatusResult{OrderID: orderID, Status: "RUNNING", Version: 1, UpdatedAt: time.Now()}, nil
	}
	hub := statusstream.NewHub(2)
	server := sseServer(t, &funcStatusService{status: running}, hub)

	var cancels []context.CancelFunc
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/checkouts/"+orderID.String()+"/events", nil)
		go func() {
			if response, err := http.DefaultClient.Do(request); err == nil {
				io.Copy(io.Discard, response.Body)
				response.Body.Close()
			}
		}()
	}
	waitForSubscribers(t, hub, 2)

	if code, _ := readAll(t, server.URL+"/v1/checkouts/"+orderID.String()+"/events"); code != http.StatusServiceUnavailable {
		t.Fatalf("third stream status %d, want 503 at capacity", code)
	}
	cancels[0]()
	waitForSubscribers(t, hub, 1)
	cancels[1]()
	waitForSubscribers(t, hub, 0)
}

func TestClientDisconnectReleasesTheSubscriptionAndDoesNotLeakGoroutines(t *testing.T) {
	orderID := uuid.New()
	service := &funcStatusService{status: func(int) (checkout.CheckoutStatusResult, error) {
		return checkout.CheckoutStatusResult{OrderID: orderID, Status: "RUNNING", Version: 1, UpdatedAt: time.Now()}, nil
	}}
	hub := statusstream.NewHub(500)
	server := sseServer(t, service, hub)
	http.DefaultClient.CloseIdleConnections()
	time.Sleep(50 * time.Millisecond)
	before := runtime.NumGoroutine()

	const streams = 60
	cancels := make([]context.CancelFunc, streams)
	var wg sync.WaitGroup
	for i := 0; i < streams; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancels[i] = cancel
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/checkouts/"+orderID.String()+"/events", nil)
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, err := (&http.Client{Transport: &http.Transport{DisableKeepAlives: true}}).Do(request)
			if err != nil {
				return
			}
			defer response.Body.Close()
			io.Copy(io.Discard, response.Body)
		}()
	}
	waitForSubscribers(t, hub, streams)
	for _, cancel := range cancels {
		cancel() // the browser tab closes
	}
	wg.Wait()

	waitForSubscribers(t, hub, 0)
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before+5 && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before+5 {
		buffer := make([]byte, 1<<16)
		t.Fatalf("goroutines: %d before, %d after %d disconnects\n%s", before, after, streams, buffer[:runtime.Stack(buffer, true)])
	}
}

func TestDuplicateAndStaleNotificationsNeverEmitRepeatedOrOlderVersions(t *testing.T) {
	orderID := uuid.New()
	versions := []int64{3, 3, 2, 3, 5, 4, 6} // repeats, then older reads from a lagging replica
	service := &funcStatusService{status: func(call int) (checkout.CheckoutStatusResult, error) {
		index := call
		if index >= len(versions) {
			index = len(versions) - 1
		}
		status := "RUNNING"
		if versions[index] == 6 {
			status = "COMPLETED"
		}
		return checkout.CheckoutStatusResult{OrderID: orderID, Status: status, Version: versions[index], UpdatedAt: time.Now()}, nil
	}}
	hub := statusstream.NewHub(10)
	server := sseServer(t, service, hub)

	done := make(chan string, 1)
	go func() { _, body := readAll(t, server.URL+"/v1/checkouts/"+orderID.String()+"/events"); done <- body }()
	waitForSubscribers(t, hub, 1)
	for i := 0; i < len(versions)-1; i++ {
		hub.Notify(orderID.String())
		time.Sleep(15 * time.Millisecond)
	}

	select {
	case body := <-done:
		got := fmt.Sprint(eventIDs(body))
		if got != "[3 5 6]" {
			t.Fatalf("event ids = %s, want [3 5 6]: strictly increasing, no repeats, older reads ignored\n%s", got, body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not finish after the terminal version")
	}
	waitForSubscribers(t, hub, 0)
}

func TestMidStreamSnapshotFailureEndsTheStreamAndReleasesTheSubscription(t *testing.T) {
	orderID := uuid.New()
	service := &funcStatusService{status: func(call int) (checkout.CheckoutStatusResult, error) {
		if call == 0 {
			return checkout.CheckoutStatusResult{OrderID: orderID, Status: "RUNNING", Version: 1, UpdatedAt: time.Now()}, nil
		}
		return checkout.CheckoutStatusResult{}, errors.New("connection reset")
	}}
	hub := statusstream.NewHub(10)
	server := sseServer(t, service, hub)

	done := make(chan string, 1)
	go func() { _, body := readAll(t, server.URL+"/v1/checkouts/"+orderID.String()+"/events"); done <- body }()
	waitForSubscribers(t, hub, 1)
	hub.Notify(orderID.String())

	select {
	case body := <-done:
		if ids := eventIDs(body); len(ids) != 1 || ids[0] != "1" {
			t.Fatalf("events = %v, want just the first snapshot before the stream ends", ids)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream stayed open after its snapshot read failed: the client would wait forever on a dead stream")
	}
	waitForSubscribers(t, hub, 0)
}

func TestManyConcurrentStreamsAllReceiveTheTerminalEventAndLeaveNothingBehind(t *testing.T) {
	const orders = 40
	var mu sync.Mutex
	done := map[uuid.UUID]bool{}
	service := &funcStatusServiceByOrder{state: func(id uuid.UUID) checkout.CheckoutStatusResult {
		mu.Lock()
		defer mu.Unlock()
		if done[id] {
			return checkout.CheckoutStatusResult{OrderID: id, Status: "COMPLETED", Version: 4, UpdatedAt: time.Now()}
		}
		return checkout.CheckoutStatusResult{OrderID: id, Status: "RUNNING", Version: 2, UpdatedAt: time.Now()}
	}}
	hub := statusstream.NewHub(1000)
	server := sseServer(t, service, hub)

	ids := make([]uuid.UUID, orders)
	bodies := make([]chan string, orders)
	for i := range ids {
		ids[i], bodies[i] = uuid.New(), make(chan string, 1)
		go func() {
			_, body := readAll(t, server.URL+"/v1/checkouts/"+ids[i].String()+"/events")
			bodies[i] <- body
		}()
	}
	waitForSubscribers(t, hub, orders)
	for _, id := range ids {
		mu.Lock()
		done[id] = true
		mu.Unlock()
		hub.Notify(id.String())
	}

	for i, ch := range bodies {
		select {
		case body := <-ch:
			if got := fmt.Sprint(eventIDs(body)); got != "[2 4]" || !strings.Contains(body, `"status":"COMPLETED"`) {
				t.Fatalf("stream %d events = %s, want [2 4] ending COMPLETED\n%s", i, got, body)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("stream %d never finished", i)
		}
	}
	waitForSubscribers(t, hub, 0)
}

type funcStatusServiceByOrder struct {
	sseCheckoutService
	state func(uuid.UUID) checkout.CheckoutStatusResult
}

func (s *funcStatusServiceByOrder) GetCheckoutStatus(_ context.Context, id uuid.UUID) (checkout.CheckoutStatusResult, error) {
	return s.state(id), nil
}

// A raw reader makes sure the heartbeat comment is not mistaken for an event.
func TestHeartbeatCommentsAreNotEventsAndKeepAnIdleStreamAlive(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for a heartbeat")
	}
	orderID := uuid.New()
	service := &funcStatusService{status: func(int) (checkout.CheckoutStatusResult, error) {
		return checkout.CheckoutStatusResult{OrderID: orderID, Status: "RUNNING", Version: 1, UpdatedAt: time.Now()}, nil
	}}
	hub := statusstream.NewHub(10)
	server := sseServer(t, service, hub)

	ctx, cancel := context.WithTimeout(context.Background(), sseHeartbeat+5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/checkouts/"+orderID.String()+"/events", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if ct := response.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q, want text/event-stream", ct)
	}
	if response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatal("proxy buffering must be disabled so events are not held back")
	}

	scanner := bufio.NewScanner(response.Body)
	sawEvent, sawHeartbeat := false, false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			sawEvent = true
		}
		if strings.HasPrefix(line, ": heartbeat") {
			sawHeartbeat = true
			break
		}
	}
	if !sawEvent || !sawHeartbeat {
		t.Fatalf("event=%v heartbeat=%v, want the snapshot then a heartbeat comment", sawEvent, sawHeartbeat)
	}
}
