package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

type checkoutResponse struct {
	OrderID string `json:"order_id"`
}

func main() {
	ctx := context.Background()
	db, err := pgx.Connect(ctx, required("DATABASE_URL"))
	must(err)
	defer db.Close(ctx)

	var first, second string
	must(db.QueryRow(ctx, `SELECT id::text FROM inventory.products ORDER BY created_at,id LIMIT 1`).Scan(&first))
	must(db.QueryRow(ctx, `SELECT id::text FROM inventory.products ORDER BY created_at,id OFFSET 1 LIMIT 1`).Scan(&second))

	transport := &http.Transport{MaxIdleConns: 1200, MaxIdleConnsPerHost: 1200, MaxConnsPerHost: 1200}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	url := required("ORCHESTRATOR_URL") + "/v1/checkouts"
	key := fmt.Sprintf("phase2-storm-%d", time.Now().UnixNano())
	body := encode(map[string]any{"items": []map[string]any{{"product_id": first, "quantity": 1}}})

	const attempts = 1000
	start := make(chan struct{})
	results := make(chan checkoutResponse, attempts)
	errors := make(chan error, attempts)
	statuses := make(chan int, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			status, response, err := checkout(client, url, key, body)
			if err != nil {
				errors <- err
				return
			}
			statuses <- status
			results <- response
		}()
	}
	started := time.Now()
	close(start)
	wg.Wait()
	close(results)
	close(errors)
	close(statuses)

	unique := map[string]bool{}
	for result := range results {
		unique[result.OrderID] = true
	}
	statusCounts := map[int]int{}
	for status := range statuses {
		statusCounts[status]++
	}
	errorCount := 0
	for range errors {
		errorCount++
	}
	if errorCount != 0 || len(unique) != 1 || statusCounts[200]+statusCounts[202] != attempts {
		fail("storm mismatch: errors=%d unique_order_ids=%d statuses=%v", errorCount, len(unique), statusCounts)
	}
	var orderID string
	for id := range unique {
		orderID = id
	}

	conflictBody := encode(map[string]any{"items": []map[string]any{{"product_id": second, "quantity": 1}}})
	conflict, _, err := checkout(client, url, key, conflictBody)
	must(err)
	if conflict != http.StatusConflict && conflict != http.StatusUnprocessableEntity {
		fail("same key with a different body returned HTTP %d, want 409 or 422", conflict)
	}

	deadline := time.Now().Add(60 * time.Second)
	for {
		var status string
		must(db.QueryRow(ctx, `SELECT status FROM orchestrator.sagas WHERE id=$1`, orderID).Scan(&status))
		if status == "COMPLETED" || status == "COMPENSATED" {
			break
		}
		if time.Now().After(deadline) {
			fail("saga %s did not become terminal", orderID)
		}
		time.Sleep(100 * time.Millisecond)
	}

	checks := map[string]string{
		"saga":        `SELECT count(*) FROM orchestrator.sagas WHERE id=$1`,
		"order":       `SELECT count(*) FROM orders.orders WHERE id=$1`,
		"payment":     `SELECT count(*) FROM payments.payment_attempts WHERE order_id=$1`,
		"reservation": `SELECT count(*) FROM inventory.reservations WHERE order_id=$1`,
	}
	for name, query := range checks {
		var count int
		must(db.QueryRow(ctx, query, orderID).Scan(&count))
		if count != 1 {
			fail("%s rows=%d, want 1", name, count)
		}
	}

	fmt.Printf("PASS idempotency storm: attempts=%d unique_order=1 statuses=%v different_body=%d duration=%s order_id=%s\n",
		attempts, statusCounts, conflict, time.Since(started).Round(time.Millisecond), orderID)
}

func checkout(client *http.Client, url, key string, body []byte) (int, checkoutResponse, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, checkoutResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	response, err := client.Do(req)
	if err != nil {
		return 0, checkoutResponse{}, err
	}
	defer response.Body.Close()
	var result checkoutResponse
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			return response.StatusCode, result, err
		}
	} else {
		_, _ = io.Copy(io.Discard, response.Body)
	}
	return response.StatusCode, result, nil
}

func encode(value any) []byte {
	result, err := json.Marshal(value)
	must(err)
	return result
}

func required(name string) string {
	value := os.Getenv(name)
	if value == "" {
		fail("%s is required", name)
	}
	return value
}

func must(err error) {
	if err != nil {
		fail("%v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL idempotency storm: "+format+"\n", args...)
	os.Exit(1)
}
