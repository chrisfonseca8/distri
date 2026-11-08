package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var composeFile = "/home/no-good-coder08/Desktop/code/projects/distri/compose.local.yaml"

func main() {
	caseName := flag.String("case", "", "K1 or K2")
	flag.Parse()
	ctx := context.Background()
	db, err := pgx.Connect(ctx, required("DATABASE_URL"))
	must(err)
	defer db.Close(ctx)
	var product string
	must(db.QueryRow(ctx, `SELECT id::text FROM inventory.products ORDER BY created_at,id LIMIT 1`).Scan(&product))
	switch strings.ToUpper(*caseName) {
	case "K1":
		runK1(ctx, db, product)
	case "K2":
		runK2(ctx, db, product)
	default:
		fail("-case must be K1 or K2")
	}
}

func runK1(ctx context.Context, db *pgx.Conn, product string) {
	compose("stop", "payment-service")
	orderID := checkout(product)
	plan := map[string]any{
		"attempts": []map[string]any{{"at": "8s", "outcome": "capture", "capture_after": "500ms"}},
		"delivery": map[string]any{},
	}
	request(http.MethodPut, "http://127.0.0.1:9090/_sim/plan/"+orderID, plan)
	compose("start", "payment-service")

	waitFor(90*time.Second, func() bool {
		return scalar(db, `SELECT count(*) FROM orders.orders WHERE id=$1 AND status='CANCELLED'`, orderID) == 1 &&
			scalar(db, `SELECT count(*) FROM orchestrator.sagas WHERE id=$1 AND status='COMPENSATED'`, orderID) == 1 &&
			scalar(db, `SELECT count(*) FROM inventory.reservations WHERE order_id=$1 AND status='RELEASED'`, orderID) == 1 &&
			scalar(db, `SELECT count(*) FROM payments.compensation_records WHERE order_id=$1 AND status='PROCESSED'`, orderID) == 1
	}, "late capture did not refund and compensate")
	assertOneCompensation(db, orderID)
	time.Sleep(3 * time.Second)
	assertOneCompensation(db, orderID)
	if scalar(db, `SELECT count(*) FROM orchestrator.sagas WHERE id=$1 AND status='COMPLETED'`, orderID) != 0 {
		fail("K1 saga completed after its refund")
	}
	fmt.Printf("PASS K1 late success: order=%s CANCELLED, saga=COMPENSATED, reservation=RELEASED, refund_records=1\n", orderID)
}

func runK2(ctx context.Context, db *pgx.Conn, product string) {
	orderID := checkout(product)
	waitFor(20*time.Second, func() bool {
		return scalar(db, `SELECT count(*) FROM orders.orders WHERE id=$1`, orderID) == 1 &&
			scalar(db, `SELECT count(*) FROM orchestrator.sagas WHERE id=$1 AND current_step='TAKE_PAYMENT'`, orderID) == 1
	}, "K2 did not reach TAKE_PAYMENT")
	compose("stop", "order-service")
	orderStopped := true
	defer func() {
		if orderStopped {
			compose("start", "order-service")
		}
	}()

	waitFor(90*time.Second, func() bool {
		return scalar(db, `SELECT count(*) FROM orchestrator.sagas WHERE id=$1 AND current_step='CANCEL_PAID_ORDER'`, orderID) == 1
	}, "K2 did not run REFUND_PAYMENT then reach CANCEL_PAID_ORDER")
	if scalar(db, `SELECT count(*) FROM payments.compensation_records WHERE order_id=$1 AND status='PROCESSED'`, orderID) != 1 {
		fail("K2 refund was not processed exactly once before cancellation")
	}
	compose("start", "order-service")
	orderStopped = false
	waitFor(60*time.Second, func() bool {
		return scalar(db, `SELECT count(*) FROM orchestrator.sagas WHERE id=$1 AND status='COMPENSATED'`, orderID) == 1 &&
			scalar(db, `SELECT count(*) FROM orders.orders WHERE id=$1 AND status='CANCELLED'`, orderID) == 1 &&
			scalar(db, `SELECT count(*) FROM inventory.reservations WHERE order_id=$1 AND status='RELEASED'`, orderID) == 1
	}, "K2 did not finish cancellation and stock release")
	for _, step := range []string{"REFUND_PAYMENT", "CANCEL_PAID_ORDER", "RELEASE_STOCK"} {
		if scalar(db, `SELECT count(*) FROM orchestrator.saga_steps WHERE saga_id=$1 AND step=$2 AND status='SUCCEEDED'`, orderID, step) != 1 {
			fail("K2 step %s did not succeed exactly once", step)
		}
	}
	assertOneCompensation(db, orderID)
	time.Sleep(3 * time.Second)
	if scalar(db, `SELECT count(*) FROM orchestrator.sagas WHERE id=$1 AND status='COMPLETED'`, orderID) != 0 {
		fail("K2 saga completed after refund")
	}
	fmt.Printf("PASS K2 bounded refund: order=%s REFUND_PAYMENT -> CANCEL_PAID_ORDER -> RELEASE_STOCK; terminal COMPENSATED\n", orderID)
}

func checkout(product string) string {
	body := map[string]any{"items": []map[string]any{{"product_id": product, "quantity": 1}}}
	encoded, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8083/v1/checkouts", bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprintf("phase2-critical-%d", time.Now().UnixNano()))
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	must(err)
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		fail("checkout returned HTTP %d", response.StatusCode)
	}
	var result struct {
		OrderID string `json:"order_id"`
	}
	must(json.NewDecoder(response.Body).Decode(&result))
	return result.OrderID
}

func request(method, url string, body any) {
	encoded, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, url, bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	must(err)
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		fail("%s %s returned HTTP %d", method, url, response.StatusCode)
	}
}

func compose(action, service string) {
	command := exec.Command("docker", "compose", "-f", composeFile, "-f", strings.TrimSuffix(composeFile, "compose.local.yaml")+"compose.simulator.yaml", action, service)
	output, err := command.CombinedOutput()
	if err != nil {
		fail("compose %s %s: %v: %s", action, service, err, output)
	}
}

func scalar(db *pgx.Conn, query string, args ...any) int {
	var result int
	must(db.QueryRow(context.Background(), query, args...).Scan(&result))
	return result
}

func assertOneCompensation(db *pgx.Conn, orderID string) {
	if scalar(db, `SELECT count(*) FROM payments.compensation_records WHERE order_id=$1`, orderID) != 1 {
		fail("order %s does not have exactly one compensation record", orderID)
	}
}

func waitFor(timeout time.Duration, condition func() bool, message string) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	fail(message)
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
	fmt.Fprintf(os.Stderr, "FAIL critical path: "+format+"\n", args...)
	os.Exit(1)
}
