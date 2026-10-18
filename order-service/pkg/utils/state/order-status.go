package state

type OrderStatus string

const (
	OrderPendingPayment OrderStatus = "PENDING_PAYMENT"
	OrderPaid           OrderStatus = "PAID"
	OrderCancelled      OrderStatus = "CANCELLED"
	OrderExpired        OrderStatus = "EXPIRED"
)

// orderTransitions lists the allowed outgoing transitions for every known state.
// PAID, CANCELLED, and EXPIRED are terminal states.
var orderTransitions = map[OrderStatus]map[OrderStatus]bool{
	OrderPendingPayment: {
		OrderPaid:      true,
		OrderCancelled: true,
		OrderExpired:   true,
	},
	OrderPaid:      {},
	OrderCancelled: {},
	OrderExpired:   {},
}

// CanTransition reports whether a transition between two order states is allowed.
func CanTransition(from, to OrderStatus) bool {
	return orderTransitions[from][to]
}
