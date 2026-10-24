// razorpay-check makes a few real Razorpay Test Mode API calls to prove that the
// credentials in .env work and that the adapter behaves as the code expects.
//
//	go run ./cmd/razorpay-check                    # one unpaid test order
//	go run ./cmd/razorpay-check -probe-duplicate   # plus a duplicate-receipt probe (a second order)
//
// It creates ONE unpaid test order (Razorpay orders cannot be deleted; no money
// moves in Test Mode) and never prints the key secret.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/pkg/payment_gateway"
	"github.com/joho/godotenv"
)

const checkAmountPaise = 10000 // INR 100.00, well above Razorpay's minimum

type checker struct{ failed int }

func (c *checker) step(name string, ok bool, detail string) {
	status := "PASS"
	if !ok {
		status = "FAIL"
		c.failed++
	}
	fmt.Printf("%s  %s\n      %s\n", status, name, detail)
}

func main() {
	if err := loadEnv(); err != nil {
		fmt.Fprintln(os.Stderr, "load .env:", err)
		os.Exit(2)
	}
	gateway, err := paymentgateway.NewRazorpayFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration problem:", err)
		os.Exit(2)
	}
	fmt.Printf("Razorpay key id: %s\n\n", maskKeyID(os.Getenv("RAZORPAY_KEY_ID")))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := &checker{}
	orderID := uuid.New()

	// 1. Create an order. A successful create is "awaiting payment", never success.
	_, err = gateway.CreateCharge(ctx, orderID, checkAmountPaise)
	var awaiting *paymentgateway.ErrAwaitingPayment
	created := errors.As(err, &awaiting)
	detail := fmt.Sprint(err)
	if created {
		detail = "Razorpay order " + awaiting.Ref + " for receipt " + orderID.String()
	}
	c.step("1. create order (credentials accepted, order awaits payment)", created, detail)
	if !created {
		fmt.Println("\nCannot continue: the create call failed. Check the key id/secret and that they are Test Mode keys.")
		os.Exit(1)
	}

	// 2. Look it up by receipt, as the reconciler does. Razorpay's receipt filter
	// is eventually consistent, so poll and report how long the order took to
	// show up (the reconciler tolerates this: it waits out the payment window).
	var state paymentgateway.OrderState
	lookupStarted := time.Now()
	for {
		state, err = gateway.LookupOrder(ctx, orderID)
		if !errors.Is(err, paymentgateway.ErrOrderNotFound) || time.Since(lookupStarted) > 45*time.Second {
			break
		}
		time.Sleep(2 * time.Second)
	}
	c.step("2. look the order up by receipt",
		err == nil && state.GatewayOrderID == awaiting.Ref && state.AmountPaise == checkAmountPaise && state.CapturedPaymentID == "",
		fmt.Sprintf("visible after %s; state=%+v err=%v", time.Since(lookupStarted).Round(time.Second), state, err))

	// 3. (optional) Is receipt uniqueness enforced? The docs say a repeated
	// receipt is rejected; the real API was observed to accept it. This creates a
	// second test order, so it only runs with -probe-duplicate.
	if len(os.Args) > 1 && os.Args[1] == "-probe-duplicate" {
		_, err = gateway.CreateCharge(ctx, orderID, checkAmountPaise)
		var again *paymentgateway.ErrAwaitingPayment
		switch {
		case errors.As(err, &again) && again.Ref == awaiting.Ref:
			fmt.Printf("INFO  3. receipt uniqueness IS enforced: the duplicate create returned the existing order\n\n")
		case errors.As(err, &again):
			fmt.Printf("INFO  3. receipt uniqueness is NOT enforced: a duplicate create made a second order (%s)\n\n", again.Ref)
		default:
			fmt.Printf("INFO  3. duplicate create result: %v\n\n", err)
		}
	}

	// 4. An unknown receipt must report "not found".
	_, err = gateway.LookupOrder(ctx, uuid.New())
	c.step("4. unknown receipt is reported as not found",
		errors.Is(err, paymentgateway.ErrOrderNotFound), fmt.Sprintf("err=%v", err))

	// 5. Wrong secret: must be an ambiguous error, never a rejection or success.
	bad, err := paymentgateway.NewRazorpay(paymentgateway.RazorpayConfig{
		KeyID:     os.Getenv("RAZORPAY_KEY_ID"),
		KeySecret: "definitely-not-the-secret",
		BaseURL:   os.Getenv("RAZORPAY_BASE_URL"),
	})
	if err == nil {
		_, err = bad.CreateCharge(ctx, uuid.New(), checkAmountPaise)
		var rejected *paymentgateway.ErrRejected
		var ok *paymentgateway.ErrAwaitingPayment
		c.step("5. wrong secret is an ambiguous error (not a rejection)",
			err != nil && !errors.As(err, &rejected) && !errors.As(err, &ok),
			fmt.Sprintf("err=%v", err))
	}

	fmt.Println()
	if c.failed > 0 {
		fmt.Printf("%d check(s) failed.\n", c.failed)
		os.Exit(1)
	}
	fmt.Println("All checks passed. One unpaid test order was created in your Razorpay Test Mode dashboard.")
}

func loadEnv() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	for {
		path := filepath.Join(dir, ".env")
		if _, err := os.Stat(path); err == nil {
			return godotenv.Load(path)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

func maskKeyID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 12 {
		return "(too short to show)"
	}
	return id[:9] + "…" + id[len(id)-4:]
}
