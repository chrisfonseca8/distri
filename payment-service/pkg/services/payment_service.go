package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidAmount   = errors.New("amount_paise must be greater than zero")
	ErrInvalidCurrency = errors.New("currency must contain three characters")
	ErrPaymentNotFound = errors.New("payment not found")
)

type CreatePaymentInput struct {
	OrderID     uuid.UUID
	AmountPaise int64
	Currency    string
}

type CreatePaymentResult struct {
	Attempt models.PaymentAttempt
	Created bool
}

type PaymentService struct {
	db *gorm.DB
}

func NewPaymentService(db *gorm.DB) *PaymentService {
	return &PaymentService{db: db}
}

func (s *PaymentService) CreatePayment(ctx context.Context, input CreatePaymentInput) (CreatePaymentResult, error) {
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

	attempt := models.PaymentAttempt{
		OrderID:     input.OrderID,
		AmountPaise: input.AmountPaise,
		Currency:    currency,
		Status:      "CREATED",
		Gateway:     "sim",
	}

	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "order_id"}},
		DoNothing: true,
	}).Create(&attempt)
	if result.Error != nil {
		return CreatePaymentResult{}, fmt.Errorf("create payment: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		if err := s.db.WithContext(ctx).Where("order_id = ?", input.OrderID).First(&attempt).Error; err != nil {
			return CreatePaymentResult{}, fmt.Errorf("get existing payment: %w", err)
		}
		return CreatePaymentResult{Attempt: attempt, Created: false}, nil
	}

	return CreatePaymentResult{Attempt: attempt, Created: true}, nil
}

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
