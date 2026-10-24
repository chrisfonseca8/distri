package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type loadClient struct {
	checkoutURL string // orchestrator-service: checkout entry point and saga status

	http     *http.Client
	metrics  *collector
	behavior string
	sequence atomic.Uint64
}
type cartItem struct {
	ProductID string `json:"product_id"`
	Quantity  int64  `json:"quantity"`
}
type checkoutResponse struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}
type sagaResponse struct {
	Status string `json:"status"`
}
type orderResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// workflow runs one checkout and payment workflow for a single product.
func (c *loadClient) workflow(parent context.Context, productID string) {
	c.workflowCart(parent, []cartItem{{ProductID: productID, Quantity: 1}})
}

// workflowCart runs one workflow for a cart, using the duplicate-checkout variant when that behaviour is set.
func (c *loadClient) workflowCart(parent context.Context, items []cartItem) {
	sequence := c.sequence.Add(1) - 1
	switch c.behavior {
	case "duplicate-checkout":
		c.duplicateCheckoutWorkflow(parent, items, sequence)
	default:
		c.standardWorkflow(parent, items)
	}
}

// standardWorkflow runs checkout, then payment, then waits for the order to reach a final status.
func (c *loadClient) standardWorkflow(parent context.Context, items []cartItem) {
	c.metrics.beginWorkflow()
	defer c.metrics.endWorkflow()
	started := time.Now()
	c.metrics.offeredOne()
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	checkoutStatus, checkout, latency, transport, decode := c.checkout(ctx, items)
	c.metrics.checkoutResult(checkoutStatus, latency, transport, decode)
	if transport != nil || decode != nil || checkoutStatus < 200 || checkoutStatus >= 300 || checkout.OrderID == "" {
		return
	}
	c.metrics.acceptedOne()
	c.finishOrder(ctx, started, checkout.OrderID)
}

type checkoutCall struct {
	status    int
	result    checkoutResponse
	latency   time.Duration
	transport error
	decode    error
}

// duplicateCheckoutWorkflow sends some checkouts twice with the same key, sometimes at the same moment, to test idempotency.
func (c *loadClient) duplicateCheckoutWorkflow(parent context.Context, items []cartItem, sequence uint64) {
	c.metrics.beginWorkflow()
	defer c.metrics.endWorkflow()
	started := time.Now()
	c.metrics.offeredOne()
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	key := "load-" + newUUID()
	first := func() checkoutCall {
		status, result, latency, transport, decode := c.checkoutWithKey(ctx, items, key)
		c.metrics.checkoutResult(status, latency, transport, decode)
		return checkoutCall{status, result, latency, transport, decode}
	}
	var calls []checkoutCall
	if sequence%5 == 0 {
		c.metrics.duplicate(1, 1, 0, 0)
		if sequence%10 == 0 {
			calls = make([]checkoutCall, 2)
			var wait sync.WaitGroup
			start := make(chan struct{})
			wait.Add(2)
			for index := range calls {
				go func(index int) { defer wait.Done(); <-start; calls[index] = first() }(index)
			}
			close(start)
			wait.Wait()
		} else {
			calls = []checkoutCall{first(), first()}
		}
	} else {
		calls = []checkoutCall{first()}
	}
	c.finishCheckoutCalls(ctx, started, calls)
}

// finishCheckoutCalls checks that repeated checkouts agree and then continues the workflow.
func (c *loadClient) finishCheckoutCalls(ctx context.Context, started time.Time, calls []checkoutCall) {
	var chosen checkoutResponse
	valid, replay200, mismatch := 0, int64(0), int64(0)
	for _, call := range calls {
		if call.transport == nil && call.decode == nil && call.status >= 200 && call.status < 300 && call.result.OrderID != "" {
			valid++
			if call.status == http.StatusOK {
				replay200++
			}
			if chosen.OrderID == "" {
				chosen = call.result
			} else if chosen.OrderID != call.result.OrderID {
				mismatch++
			}
		}
	}
	if len(calls) > 1 {
		c.metrics.duplicate(0, 0, replay200, mismatch)
	}
	if valid == 0 || mismatch > 0 {
		return
	}
	c.metrics.acceptedOne()
	c.finishOrder(ctx, started, chosen.OrderID)
}

// finishOrder waits for the saga to drive the order to a terminal state. There
// is no pay call: the orchestrator takes the payment itself. An order still
// pending when the workflow budget ends (for example an ambiguous payment) is
// counted as pending.
func (c *loadClient) finishOrder(ctx context.Context, started time.Time, orderID string) {
	order := c.waitTerminal(ctx, orderID)
	if order.Status == "PAID" || order.Status == "CANCELLED" {
		c.metrics.completedOne(order.Status, time.Since(started))
	} else {
		c.metrics.pendingOne()
	}
}

// checkout sends a checkout with a new idempotency key.
func (c *loadClient) checkout(ctx context.Context, items []cartItem) (int, checkoutResponse, time.Duration, error, error) {
	return c.checkoutWithKey(ctx, items, "load-"+newUUID())
}

// checkoutWithKey sends a checkout with the given idempotency key.
func (c *loadClient) checkoutWithKey(ctx context.Context, items []cartItem, key string) (int, checkoutResponse, time.Duration, error, error) {
	body := map[string]any{"items": items}
	var result checkoutResponse
	status, latency, transport, decode := c.do(ctx, http.MethodPost, c.checkoutURL+"/v1/checkouts", body, map[string]string{"Idempotency-Key": key}, &result)
	return status, result, latency, transport, decode
}

// waitTerminal polls the saga until it finishes. A COMPLETED saga means the
// order is PAID and the stock confirmed; COMPENSATED means CANCELLED and the
// stock released. Reporting the order as done any earlier would race the saga's
// last inventory step.
func (c *loadClient) waitTerminal(ctx context.Context, orderID string) orderResponse {
	for {
		select {
		case <-ctx.Done():
			return orderResponse{}
		case <-time.After(100 * time.Millisecond):
		}
		var result sagaResponse
		status, _, transport, decode := c.do(ctx, http.MethodGet, c.checkoutURL+"/v1/checkouts/"+orderID, nil, nil, &result)
		if transport != nil || decode != nil || status != 200 {
			continue
		}
		switch result.Status {
		case "COMPLETED":
			return orderResponse{ID: orderID, Status: "PAID"}
		case "COMPENSATED":
			return orderResponse{ID: orderID, Status: "CANCELLED"}
		}
	}
}

// do sends a JSON request and returns the status, latency, transport error and decode error.
func (c *loadClient) do(ctx context.Context, method, url string, body any, headers map[string]string, out any) (int, time.Duration, error, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, 0, err, nil
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return 0, 0, err, nil
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	c.metrics.beginRequest()
	started := time.Now()
	response, err := c.http.Do(req)
	latency := time.Since(started)
	c.metrics.endRequest()
	if err != nil {
		return 0, latency, err, nil
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return response.StatusCode, latency, nil, err
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return response.StatusCode, latency, nil, err
		}
	}
	return response.StatusCode, latency, nil, nil
}

// newUUID returns a random version-4 UUID.
func newUUID() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	data[6] = (data[6] & 15) | 64
	data[8] = (data[8] & 63) | 128
	v := hex.EncodeToString(data)
	return fmt.Sprintf("%s-%s-%s-%s-%s", v[:8], v[8:12], v[12:16], v[16:20], v[20:])
}
