package services

import (
	"errors"
	"fmt"
	"math"
	"testing"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/pkg/messages"
	"github.com/chrisfonseca08/distri/order-service/pkg/messaging"
)

type testSQLStateError struct {
	state string
}

func (e testSQLStateError) Error() string    { return "database error" }
func (e testSQLStateError) SQLState() string { return e.state }

func TestValidateOrderCreate(t *testing.T) {
	productID := uuid.New()
	valid := messages.OrderCreate{
		OrderID:    uuid.New(),
		Currency:   "INR",
		TotalPaise: 10000,
		Items: []messages.OrderCreateItem{{
			ProductID: productID, Quantity: 2, UnitPricePaise: 5000,
		}},
	}

	tests := []struct {
		name   string
		mutate func(*messages.OrderCreate)
	}{
		{name: "missing order ID", mutate: func(c *messages.OrderCreate) { c.OrderID = uuid.Nil() }},
		{name: "missing currency", mutate: func(c *messages.OrderCreate) { c.Currency = "" }},
		{name: "missing items", mutate: func(c *messages.OrderCreate) { c.Items = nil }},
		{name: "nonpositive quantity", mutate: func(c *messages.OrderCreate) { c.Items[0].Quantity = 0 }},
		{name: "nonpositive price", mutate: func(c *messages.OrderCreate) { c.Items[0].UnitPricePaise = 0 }},
		{name: "wrong total", mutate: func(c *messages.OrderCreate) { c.TotalPaise++ }},
		{name: "overflow", mutate: func(c *messages.OrderCreate) {
			c.Items[0].Quantity = math.MaxInt64
			c.Items[0].UnitPricePaise = 2
		}},
		{name: "duplicate product", mutate: func(c *messages.OrderCreate) {
			c.Items = append(c.Items, c.Items[0])
			c.TotalPaise *= 2
		}},
	}

	if err := validateOrderCreate(valid); err != nil {
		t.Fatalf("valid command rejected: %v", err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := valid
			command.Items = append([]messages.OrderCreateItem(nil), valid.Items...)
			test.mutate(&command)
			if err := validateOrderCreate(command); err == nil {
				t.Fatal("invalid command accepted")
			}
		})
	}
}

func TestOrderCreateHandlerClassifiesInvalidMessagesAsPermanent(t *testing.T) {
	handler := NewOrderCreateHandler(nil)
	tests := []messaging.Delivery{
		{Body: []byte(`not-json`), MessageId: "message-id"},
		{Body: []byte(`{}`), MessageId: "message-id"},
		{Body: []byte(`{"order_id":"00000000-0000-4000-8000-000000000001","currency":"INR","total_paise":1,"items":[{"product_id":"00000000-0000-4000-8000-000000000002","quantity":1,"unit_price_paise":1}]}`)},
	}

	for _, delivery := range tests {
		err := handler.Handle(t.Context(), delivery)
		var permanent *messaging.PermanentError
		if !errors.As(err, &permanent) {
			t.Fatalf("error = %v, want PermanentError", err)
		}
	}
}

func TestIsPermanentDatabaseError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "data exception", err: testSQLStateError{state: "22003"}, want: true},
		{name: "integrity violation", err: testSQLStateError{state: "23503"}, want: true},
		{name: "wrapped integrity violation", err: fmt.Errorf("insert: %w", testSQLStateError{state: "23514"}), want: true},
		{name: "connection exception", err: testSQLStateError{state: "08006"}, want: false},
		{name: "serialization failure", err: testSQLStateError{state: "40001"}, want: false},
		{name: "non database error", err: errors.New("context canceled"), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isPermanentDatabaseError(test.err); got != test.want {
				t.Fatalf("isPermanentDatabaseError() = %t, want %t", got, test.want)
			}
		})
	}
}
