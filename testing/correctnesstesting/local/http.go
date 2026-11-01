package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// request sends a JSON request, decodes the JSON reply into out and returns the status and raw body.
func (p *phaseContext) request(method, url string, body any, headers map[string]string, out any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(p.suite.ctx, method, url, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if p.suite.runLabel != "" {
		req.Header.Set("X-Test-Run-ID", p.suite.runLabel)
	}
	started := time.Now()
	response, err := p.suite.client.Do(req)
	duration := time.Since(started)
	p.mu.Lock()
	p.latencies = append(p.latencies, duration)
	if err != nil {
		p.httpErrors++
	}
	p.mu.Unlock()
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err == nil && out != nil && len(raw) > 0 {
		err = json.Unmarshal(raw, out)
	}
	return response.StatusCode, raw, err
}

// checkout sends a checkout to the orchestrator with the given idempotency key.
func (p *phaseContext) checkout(key string, items []map[string]any) (int, checkoutResult, error) {
	var result checkoutResult
	status, _, err := p.request(http.MethodPost, p.suite.orchestratorURL+"/v1/checkouts",
		map[string]any{"items": items}, map[string]string{"Idempotency-Key": key}, &result)
	return status, result, err
}

// getOrder reads an order from order-service.
func (p *phaseContext) getOrder(id string) (int, orderResult, error) {
	var result orderResult
	status, _, err := p.request(http.MethodGet, p.suite.orderURL+"/v1/orders/"+id, nil, nil, &result)
	return status, result, err
}

// awaitOrder polls the order read model until the saga drives the order to PAID
// or CANCELLED. If that does not happen within the timeout it returns the last
// observed order, so a still-pending order is a valid result (for example after
// an ambiguous payment).
func (p *phaseContext) awaitOrder(id string, timeout time.Duration) (int, orderResult, error) {
	deadline := time.Now().Add(timeout)
	for {
		status, order, err := p.getOrder(id)
		terminal := order.Status == "PAID" || order.Status == "CANCELLED"
		if err == nil && status == 200 && terminal {
			return status, order, err
		}
		if time.Now().After(deadline) {
			return status, order, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// waitScalar polls a count query until it equals want or the timeout passes,
// and returns the last value. Orders appear asynchronously after a 202.
func (p *phaseContext) waitScalar(timeout time.Duration, want int64, query string, args ...any) int64 {
	deadline := time.Now().Add(timeout)
	for {
		value := p.scalar(query, args...)
		if value == want || time.Now().After(deadline) {
			return value
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// reserve asks inventory-service to reserve stock for an order.
func (p *phaseContext) reserve(orderID, hash string, items []map[string]any) (int, reservationResult, error) {
	var result reservationResult
	status, _, err := p.request(http.MethodPost, p.suite.inventoryURL+"/internal/v1/reservations",
		map[string]any{"order_id": orderID, "request_hash": hash, "items": items}, nil, &result)
	return status, result, err
}

// changeReservation confirms or releases an order's reservation.
func (p *phaseContext) changeReservation(orderID, action string) (int, reservationResult, error) {
	var result reservationResult
	status, _, err := p.request(http.MethodPost,
		p.suite.inventoryURL+"/internal/v1/reservations/"+orderID+"/"+action, nil, nil, &result)
	return status, result, err
}

// createPayment asks payment-service to create a payment for an order.
func (p *phaseContext) createPayment(orderID string, amount int64, currency string) (int, paymentResult, error) {
	var result paymentResult
	status, _, err := p.request(http.MethodPost, p.suite.paymentURL+"/v1/payments",
		map[string]any{"order_id": orderID, "amount_paise": amount, "currency": currency}, nil, &result)
	return status, result, err
}

// getPayment reads a payment from payment-service.
func (p *phaseContext) getPayment(id string) (int, paymentResult, error) {
	var result paymentResult
	status, _, err := p.request(http.MethodGet, p.suite.paymentURL+"/v1/payments/"+id, nil, nil, &result)
	return status, result, err
}

// waitPayment polls a payment until it leaves CREATED and PENDING, or the timeout passes.
func (p *phaseContext) waitPayment(id string, timeout time.Duration) paymentResult {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, payment, err := p.getPayment(id)
		if err == nil && payment.Status != "CREATED" && payment.Status != "PENDING" {
			return payment
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, payment, _ := p.getPayment(id)
	return payment
}

// newUUID returns a random version-4 UUID.
func newUUID() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	value := hex.EncodeToString(data)
	return fmt.Sprintf("%s-%s-%s-%s-%s", value[:8], value[8:12], value[12:16], value[16:20], value[20:])
}
