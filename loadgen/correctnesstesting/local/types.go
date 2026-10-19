package main

import "time"

type phaseDefinition struct {
	Number      int
	Name        string
	Description string
	Run         func(*phaseContext)
}

type assertionResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type metricSummary struct {
	Requests int     `json:"requests"`
	Errors   int     `json:"errors"`
	MinMS    float64 `json:"min_ms"`
	MeanMS   float64 `json:"mean_ms"`
	P50MS    float64 `json:"p50_ms"`
	P95MS    float64 `json:"p95_ms"`
	P99MS    float64 `json:"p99_ms"`
	MaxMS    float64 `json:"max_ms"`
}

type phaseResult struct {
	Number      int               `json:"number"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Status      string            `json:"status"`
	DurationMS  float64           `json:"duration_ms"`
	Metrics     metricSummary     `json:"http_metrics"`
	Assertions  []assertionResult `json:"assertions"`
	Notes       []string          `json:"notes,omitempty"`
}

type correctnessReport struct {
	Suite       string        `json:"suite"`
	RunID       string        `json:"run_id"`
	StartedAt   time.Time     `json:"started_at"`
	FinishedAt  time.Time     `json:"finished_at"`
	Environment reportEnv     `json:"environment"`
	Summary     reportSummary `json:"summary"`
	Phases      []phaseResult `json:"phases"`
}

type correctnessReportCollection struct {
	Suite     string              `json:"suite"`
	UpdatedAt time.Time           `json:"updated_at"`
	Runs      []correctnessReport `json:"runs"`
}

type reportEnv struct {
	OrderURL     string `json:"order_url"`
	InventoryURL string `json:"inventory_url"`
	PaymentURL   string `json:"payment_url"`
	GatewayMode  string `json:"gateway_mode"`
	RunLabel     string `json:"run_label,omitempty"`
}

type reportSummary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

type checkoutResult struct {
	OrderID    string `json:"order_id"`
	Status     string `json:"status"`
	TotalPaise int64  `json:"total_paise"`
	Currency   string `json:"currency"`
}

type orderResult struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	TotalPaise int64  `json:"total_paise"`
	Currency   string `json:"currency"`
}

type reservationResult struct {
	ReservationID string         `json:"reservation_id"`
	OrderID       string         `json:"order_id"`
	Status        string         `json:"status"`
	TotalPaise    int64          `json:"total_paise"`
	Currency      string         `json:"currency"`
	Items         []reservedItem `json:"items"`
}

type reservedItem struct {
	ProductID      string `json:"product_id"`
	Quantity       int64  `json:"quantity"`
	UnitPricePaise int64  `json:"unit_price_paise"`
}

type paymentResult struct {
	ID          string  `json:"id"`
	OrderID     string  `json:"order_id"`
	AmountPaise int64   `json:"amount_paise"`
	Currency    string  `json:"currency"`
	Status      string  `json:"status"`
	FailureCode *string `json:"failure_code"`
}

type inventorySnapshot struct {
	OnHand    int64
	Reserved  int64
	Allocated int64
}
