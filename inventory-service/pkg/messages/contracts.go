// Package messages defines inventory-service AMQP contracts. Message IDs live
// in the AMQP envelope, never in these JSON payloads.
package messages

import "uuid"

type Step string

const (
	CONFIRM_STOCK Step = "CONFIRM_STOCK"
	RELEASE_STOCK Step = "RELEASE_STOCK"
)

type Outcome string

const (
	Succeeded Outcome = "Succeeded"
	Failed    Outcome = "Failed"
	Ambiguous Outcome = "Ambiguous"
)

const (
	InventoryConfirmRoutingKey = "inventory.confirm"
	InventoryReleaseRoutingKey = "inventory.release"
	InventoryReplyRoutingKey   = "reply.inventory"
	InventoryCommandsQueue     = "distri.inventory.commands"
)

type InventoryConfirm struct {
	OrderID uuid.UUID `json:"order_id"`
}

type InventoryRelease struct {
	OrderID uuid.UUID `json:"order_id"`
}

type Reply struct {
	OrderID uuid.UUID `json:"order_id"`
	Step    Step      `json:"step"`
	Outcome Outcome   `json:"outcome"`
	Error   string    `json:"error,omitempty"`
}
