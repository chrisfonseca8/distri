package sim

// JSON views. Fields are pointers where the real API returns null.

type OrderView struct {
	ID         string            `json:"id"`
	Entity     string            `json:"entity"`
	Amount     int64             `json:"amount"`
	AmountPaid int64             `json:"amount_paid"`
	AmountDue  int64             `json:"amount_due"`
	Currency   string            `json:"currency"`
	Receipt    *string           `json:"receipt"`
	OfferID    *string           `json:"offer_id"`
	Status     string            `json:"status"`
	Attempts   int               `json:"attempts"`
	Notes      map[string]string `json:"notes"`
	CreatedAt  int64             `json:"created_at"`
}

// orderView converts an internal order to its API response.
func orderView(o *order) OrderView {
	view := OrderView{
		ID: o.id, Entity: "order", Amount: o.amount, AmountPaid: o.amountPaid,
		AmountDue: o.amount - o.amountPaid, Currency: o.currency, Status: o.status,
		Attempts: o.attempts, Notes: o.notes, CreatedAt: o.createdAt.Unix(),
	}
	if o.receipt != "" {
		receipt := o.receipt
		view.Receipt = &receipt
	}
	return view
}

type PaymentView struct {
	ID             string            `json:"id"`
	Entity         string            `json:"entity"`
	Amount         int64             `json:"amount"`
	Currency       string            `json:"currency"`
	Status         string            `json:"status"`
	OrderID        string            `json:"order_id"`
	Method         string            `json:"method"`
	Captured       bool              `json:"captured"`
	AmountRefunded int64             `json:"amount_refunded"`
	RefundStatus   *string           `json:"refund_status"`
	Description    *string           `json:"description"`
	ErrorCode      *string           `json:"error_code"`
	ErrorDesc      *string           `json:"error_description"`
	ErrorSource    *string           `json:"error_source"`
	ErrorStep      *string           `json:"error_step"`
	ErrorReason    *string           `json:"error_reason"`
	Fee            *int64            `json:"fee"`
	Tax            *int64            `json:"tax"`
	CreatedAt      int64             `json:"created_at"`
	Notes          map[string]string `json:"notes"`
}

// paymentView converts an internal payment to its API response.
func paymentView(p *payment) PaymentView {
	view := PaymentView{
		ID: p.id, Entity: "payment", Amount: p.amount, Currency: p.currency, Status: p.status,
		OrderID: p.orderID, Method: p.method, Captured: p.captured, AmountRefunded: p.amountRefunded,
		CreatedAt: p.createdAt.Unix(), Notes: map[string]string{},
	}
	if p.refundStatus != "" {
		status := p.refundStatus
		view.RefundStatus = &status
	}
	if p.errorCode != "" {
		view.ErrorCode, view.ErrorDesc = &p.errorCode, &p.errorDesc
		view.ErrorSource, view.ErrorStep, view.ErrorReason = &p.errorSource, &p.errorStep, &p.errorReason
	}
	if p.captured {
		zero := int64(0)
		view.Fee, view.Tax = &zero, &zero
	}
	return view
}

type RefundView struct {
	ID             string            `json:"id"`
	Entity         string            `json:"entity"`
	Amount         int64             `json:"amount"`
	Currency       string            `json:"currency"`
	PaymentID      string            `json:"payment_id"`
	Receipt        *string           `json:"receipt"`
	Status         string            `json:"status"`
	SpeedRequested *string           `json:"speed_requested,omitempty"`
	CreatedAt      int64             `json:"created_at"`
	Notes          map[string]string `json:"notes"`
}

// refundView converts an internal refund to its API response.
func refundView(r *refund) RefundView {
	notes := map[string]string{}
	for key, value := range r.notes {
		notes[key] = value
	}
	view := RefundView{
		ID: r.id, Entity: "refund", Amount: r.amount, Currency: r.currency, PaymentID: r.paymentID,
		Status: r.status, CreatedAt: r.createdAt.Unix(), Notes: notes,
	}
	if r.receipt != "" {
		receipt := r.receipt
		view.Receipt = &receipt
	}
	if r.speed != "" {
		speed := r.speed
		view.SpeedRequested = &speed
	}
	return view
}

type Collection[T any] struct {
	Entity string `json:"entity"`
	Count  int    `json:"count"`
	Items  []T    `json:"items"`
}

// collection wraps items in Razorpay's collection format.
func collection[T any](items []T) Collection[T] {
	if items == nil {
		items = []T{}
	}
	return Collection[T]{Entity: "collection", Count: len(items), Items: items}
}
