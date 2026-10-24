// Package messages defines the JSON contracts exchanged over AMQP.
//
// Every published message must set a non-empty AMQP MessageId property. The
// message ID belongs to the AMQP envelope and must not be added to these JSON
// payloads.
package messages

import "uuid"

type Step string

const (
	CREATE_ORDER         Step = "CREATE_ORDER"
	TAKE_PAYMENT         Step = "TAKE_PAYMENT"
	MARK_ORDER_PAID      Step = "MARK_ORDER_PAID"
	MARK_ORDER_CANCELLED Step = "MARK_ORDER_CANCELLED"
	CONFIRM_STOCK        Step = "CONFIRM_STOCK"
	RELEASE_STOCK        Step = "RELEASE_STOCK"
	CANCEL_PAID_ORDER    Step = "CANCEL_PAID_ORDER"
)

type Outcome string

const (
	Succeeded Outcome = "Succeeded"
	Failed    Outcome = "Failed"
	Ambiguous Outcome = "Ambiguous"
)

const (
	OrderCreateRoutingKey    = "order.create"
	OrderMarkPaidRoutingKey  = "order.mark-paid"
	OrderCancelRoutingKey    = "order.cancel"
	OrderCommandsQueue       = "distri.order.commands"
	OrderReplyRoutingKey     = "reply.order"
	OrchestratorRepliesQueue = "distri.orchestrator.replies"
)

// OrderCreateItem carries the authoritative price returned by inventory
// reservation. Prices must never be copied from a client-provided value.
type OrderCreateItem struct {
	ProductID      uuid.UUID `json:"product_id"`
	Quantity       int64     `json:"quantity"`
	UnitPricePaise int64     `json:"unit_price_paise"`
}

// OrderCreate carries totals and prices from the inventory reserve response.
type OrderCreate struct {
	OrderID    uuid.UUID         `json:"order_id"`
	Currency   string            `json:"currency"`
	TotalPaise int64             `json:"total_paise"`
	Items      []OrderCreateItem `json:"items"`
}

type OrderMarkPaid struct {
	OrderID uuid.UUID `json:"order_id"`
}

type OrderCancel struct {
	OrderID uuid.UUID `json:"order_id"`
	// Refunded marks the cancel of an order that had already been paid because its payment
	// is being refunded. Only then may a PAID order be cancelled.
	Refunded bool `json:"refunded,omitempty"`
}

type Reply struct {
	OrderID uuid.UUID `json:"order_id"`
	Step    Step      `json:"step"`
	Outcome Outcome   `json:"outcome"`
	Error   string    `json:"error,omitempty"`
}
