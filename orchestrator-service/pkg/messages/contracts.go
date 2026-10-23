// Package messages defines the JSON contracts exchanged over AMQP.
//
// Every published message must set a non-empty AMQP MessageId property. The
// message ID belongs to the AMQP envelope and must not be added to these JSON
// payloads.
package messages

import "uuid"

type Step string

const (
	CREATE_ORDER Step = "CREATE_ORDER"
)

type Outcome string

const (
	Succeeded Outcome = "Succeeded"
	Failed    Outcome = "Failed"
	Ambiguous Outcome = "Ambiguous"
)

const (
	OrderCreateRoutingKey    = "order.create"
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

type OrderReply struct {
	OrderID uuid.UUID `json:"order_id"`
	Step    Step      `json:"step"`
	Outcome Outcome   `json:"outcome"`
	Error   string    `json:"error,omitempty"`
}
