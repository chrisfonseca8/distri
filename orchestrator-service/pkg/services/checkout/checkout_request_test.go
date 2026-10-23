package checkout

import (
	"errors"
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
