// Package messages defines payment-service AMQP contracts. Message IDs live
// in the AMQP envelope, never in these JSON payloads.
package messages

import "uuid"

type Step string

const (
	TAKE_PAYMENT   Step = "TAKE_PAYMENT"
	REFUND_PAYMENT Step = "REFUND_PAYMENT"
)

type Outcome string

const (
	Succeeded Outcome = "Succeeded"
	Failed    Outcome = "Failed"
	Ambiguous Outcome = "Ambiguous"
)

const (
	PaymentInitiateRoutingKey = "payment.initiate"
	PaymentRefundRoutingKey   = "payment.refund"
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

// PaymentRefund asks for the payment that funded an order to be refunded because the
// checkout could not be completed after the money was taken.
type PaymentRefund struct {
	OrderID uuid.UUID `json:"order_id"`
}
