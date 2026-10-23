// Package messages defines payment-service AMQP contracts. Message IDs live
// in the AMQP envelope, never in these JSON payloads.
package messages

import "uuid"

type Step string

const TAKE_PAYMENT Step = "TAKE_PAYMENT"

type Outcome string

const (
	Succeeded Outcome = "Succeeded"
	Failed    Outcome = "Failed"
	Ambiguous Outcome = "Ambiguous"
)

const (
	PaymentInitiateRoutingKey = "payment.initiate"
	PaymentReplyRoutingKey    = "reply.payment"
	PaymentCommandsQueue      = "distri.payment.commands"
)

type PaymentInitiate struct {
	OrderID     uuid.UUID `json:"order_id"`
	AmountPaise int64     `json:"amount_paise"`
	Currency    string    `json:"currency"`
}

type Reply struct {
	OrderID uuid.UUID `json:"order_id"`
	Step    Step      `json:"step"`
	Outcome Outcome   `json:"outcome"`
	Error   string    `json:"error,omitempty"`
}
