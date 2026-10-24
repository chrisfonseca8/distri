package checkout

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"uuid"
)

func TestPrepareCheckoutRequestSortsBeforeHashing(t *testing.T) {
	firstProduct := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	secondProduct := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	forward, err := prepareCheckoutRequest(CreateCheckoutInput{
		IdempotencyKey: " checkout-key ",
		Items: []CheckoutItemInput{
			{ProductID: firstProduct, Quantity: 1},
			{ProductID: secondProduct, Quantity: 2},
		},
	})
	if err != nil {
		t.Fatalf("prepare forward request: %v", err)
	}

	reversed, err := prepareCheckoutRequest(CreateCheckoutInput{
		IdempotencyKey: "checkout-key",
		Items: []CheckoutItemInput{
			{ProductID: secondProduct, Quantity: 2},
			{ProductID: firstProduct, Quantity: 1},
		},
	})
	if err != nil {
		t.Fatalf("prepare reversed request: %v", err)
	}

	if forward.Attempt.RequestHash != reversed.Attempt.RequestHash {
		t.Fatalf("hashes differ: %q != %q", forward.Attempt.RequestHash, reversed.Attempt.RequestHash)
	}
	if reversed.Items[0].ProductID != firstProduct || reversed.Items[1].ProductID != secondProduct {
		t.Fatalf("items were not sorted: %+v", reversed.Items)
	}
}

func TestPrepareCheckoutRequestRejectsDuplicateProducts(t *testing.T) {
	productID := uuid.New()

	_, err := prepareCheckoutRequest(CreateCheckoutInput{
		IdempotencyKey: "checkout-key",
		Items: []CheckoutItemInput{
			{ProductID: productID, Quantity: 1},
			{ProductID: productID, Quantity: 2},
		},
	})
	if !errors.Is(err, ErrInvalidCheckoutInput) {
		t.Fatalf("error = %v, want ErrInvalidCheckoutInput", err)
	}
}

func TestPrepareRejectsBadIdempotencyKeysAndLimitsWithCleanMessages(t *testing.T) {
	product := uuid.New()
	valid := []CheckoutItemInput{{ProductID: product, Quantity: 1}}
	cases := []struct {
		name  string
		input CreateCheckoutInput
		want  string
	}{
		{"key too long", CreateCheckoutInput{IdempotencyKey: strings.Repeat("k", 256), Items: valid}, "at most 255"},
		{"key with spaces", CreateCheckoutInput{IdempotencyKey: "a b", Items: valid}, "only letters"},
		{"key with symbols", CreateCheckoutInput{IdempotencyKey: "a/b?c", Items: valid}, "only letters"},
		{"too many lines", CreateCheckoutInput{IdempotencyKey: "k", Items: manyItems(21)}, "at most 20"},
		{"quantity too large", CreateCheckoutInput{IdempotencyKey: "k", Items: []CheckoutItemInput{{ProductID: product, Quantity: 101}}}, "at most 100"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := prepareCheckoutRequest(test.input)
			if !errors.Is(err, ErrInvalidCheckoutInput) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want invalid input mentioning %q", err, test.want)
			}
		})
	}
	if _, err := prepareCheckoutRequest(CreateCheckoutInput{IdempotencyKey: strings.Repeat("k", 255), Items: manyItems(20)}); err != nil {
		t.Fatalf("a 255-character key and 20 lines are allowed: %v", err)
	}
	if _, err := prepareCheckoutRequest(CreateCheckoutInput{IdempotencyKey: "order-2026.10:24_a-b", Items: []CheckoutItemInput{{ProductID: product, Quantity: 100}}}); err != nil {
		t.Fatalf("a normal key and quantity 100 are allowed: %v", err)
	}
}

func manyItems(n int) []CheckoutItemInput {
	items := make([]CheckoutItemInput, n)
	for i := range items {
		items[i] = CheckoutItemInput{ProductID: uuid.New(), Quantity: 1}
	}
	return items
}

func TestInventoryFailuresAreShownWithoutInventorysOwnText(t *testing.T) {
	leaky := fmt.Errorf("%w: inventory returned status 400: inventory rejected the request: secret detail", ErrInvalidCheckoutInput)
	for _, err := range []error{leaky, ErrCheckoutProductNotFound, ErrCheckoutInsufficientStock, ErrCheckoutReservationInactive, errors.New("anything else")} {
		message := publicInventoryMessage(fmt.Errorf("%w: raw inventory text", err))
		if strings.Contains(message, "inventory returned") || strings.Contains(message, "raw inventory text") || strings.Contains(message, "secret") {
			t.Fatalf("message %q repeats inventory's own text", message)
		}
	}
}
