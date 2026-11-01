package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chrisfonseca08/distri/payment-service/pkg/observability"
	"gorm.io/datatypes"
	"strings"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/jobtypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidAmount   = errors.New("amount_paise must be greater than zero")
	ErrInvalidCurrency = errors.New("currency must contain three characters")
	ErrInvalidOrderID  = errors.New("order_id is required")
	ErrPaymentNotFound = errors.New("payment not found")
)

type CreatePaymentInput struct {
	OrderID     uuid.UUID
	AmountPaise int64
	Currency    string
	// Gateway is stored on the attempt; empty means the simulator ("sim").
	Gateway string
}

type CreatePaymentResult struct {
	Attempt models.PaymentAttempt
	Created bool
}

type PaymentService struct {
	db      *gorm.DB
	gateway string
}

// GatewayOption records which gateway new payment attempts belong to.
type GatewayOption func(*gatewayLabel)

type gatewayLabel struct{ name string }

// WithGatewayName sets payment_attempts.gateway for attempts created through the
// service or handler it is passed to.
func WithGatewayName(name string) GatewayOption {
	return func(label *gatewayLabel) { label.name = name }
}

// gatewayNameFrom returns the gateway name set by the options.
func gatewayNameFrom(options []GatewayOption) string {
	label := gatewayLabel{}
	for _, option := range options {
		option(&label)
	}
	return label.name
}

// NewPaymentService creates the payment service.
func NewPaymentService(db *gorm.DB, options ...GatewayOption) *PaymentService {
	return &PaymentService{db: db, gateway: gatewayNameFrom(options)}
}

// CreatePayment validates the input and creates the payment attempt for an order, or returns the existing one.
func (s *PaymentService) CreatePayment(ctx context.Context, input CreatePaymentInput) (CreatePaymentResult, error) {
	if input.OrderID == uuid.Nil() {
		return CreatePaymentResult{}, ErrInvalidOrderID
	}
	if input.AmountPaise <= 0 {
		return CreatePaymentResult{}, ErrInvalidAmount
	}

	currency := strings.ToUpper(strings.TrimSpace(input.Currency))
	if currency == "" {
		currency = "INR"
	}
	if len(currency) != 3 {
		return CreatePaymentResult{}, ErrInvalidCurrency
	}

	input.Currency = currency
	input.Gateway = s.gateway
	ctx = observability.WithFields(ctx, observability.Fields{OrderID: input.OrderID.String(), SagaID: input.OrderID.String()})
	var createResult CreatePaymentResult
	err := observability.Transaction(ctx, s.db, "create_payment", func(tx *gorm.DB) error {
		var err error
		createResult, err = createPayment(tx, input)
		return err
	})
	if err != nil {
		return CreatePaymentResult{}, err
	}
	return createResult, nil
}

// createPayment is shared by the HTTP path and the AMQP command handler. The
// caller owns the transaction so payment creation, its job, and broker effects
// can commit atomically.
func createPayment(tx *gorm.DB, input CreatePaymentInput) (CreatePaymentResult, error) {
	attempt := models.PaymentAttempt{
		OrderID:     input.OrderID,
		AmountPaise: input.AmountPaise,
		Currency:    input.Currency,
		Status:      "CREATED",
		Gateway:     gatewayOrDefault(input.Gateway),
	}
	insert := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "order_id"}},
		DoNothing: true,
	}).Create(&attempt)
	if insert.Error != nil {
		return CreatePaymentResult{}, fmt.Errorf("create payment: %w", insert.Error)
	}
	if insert.RowsAffected == 0 {
		if err := tx.Where("order_id = ?", input.OrderID).First(&attempt).Error; err != nil {
			return CreatePaymentResult{}, fmt.Errorf("get existing payment: %w", err)
		}
		return CreatePaymentResult{Attempt: attempt, Created: false}, nil
	}
	if err := enqueueJob(tx, jobtypes.ProcessPayment, attempt.ID); err != nil {
		return CreatePaymentResult{}, err
	}
	return CreatePaymentResult{Attempt: attempt, Created: true}, nil
}

// gatewayOrDefault returns name, or "sim" when it is empty.
func gatewayOrDefault(name string) string {
	if name == "" {
		return "sim"
	}
	return name
}

// enqueueJob adds a job for a payment to the queue, ignoring a duplicate.
func enqueueJob(tx *gorm.DB, jobType string, paymentID uuid.UUID) error {
	job := models.Job{Type: jobType, PaymentID: paymentID}
	if ctx := tx.Statement.Context; ctx != nil {
		if encoded, err := json.Marshal(observability.CorrelationFromContext(ctx)); err == nil {
			job.Correlation = datatypes.JSON(encoded)
		}
	}
	if err := tx.Table("payments.jobs").
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&job).Error; err != nil {
		return fmt.Errorf("enqueue %s job: %w", jobType, err)
	}
	return nil
}

// GetPayment returns one payment attempt, or ErrPaymentNotFound if it does not exist.
func (s *PaymentService) GetPayment(ctx context.Context, paymentID uuid.UUID) (models.PaymentAttempt, error) {
	var attempt models.PaymentAttempt
	result := s.db.WithContext(ctx).First(&attempt, "id = ?", paymentID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return models.PaymentAttempt{}, ErrPaymentNotFound
	}
	if result.Error != nil {
		return models.PaymentAttempt{}, fmt.Errorf("get payment: %w", result.Error)
	}

	return attempt, nil
}
