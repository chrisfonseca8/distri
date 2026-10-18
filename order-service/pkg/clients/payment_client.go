package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"uuid"
)

const defaultPaymentTimeout = 3 * time.Second

var (
	ErrPaymentInvalidRequest = errors.New("payment rejected the request")
	ErrPaymentNotFound       = errors.New("payment not found")
	ErrPaymentConflict       = errors.New("payment status conflict")
	ErrPaymentUnavailable    = errors.New("payment service unavailable")
)

type Payment struct {
	ID          uuid.UUID `json:"id"`
	OrderID     uuid.UUID `json:"order_id"`
	AmountPaise int64     `json:"amount_paise"`
	Currency    string    `json:"currency"`
	Status      string    `json:"status"`
}

type PaymentClient interface {
	Create(
		ctx context.Context,
		orderID uuid.UUID,
		amountPaise int64,
		currency string,
	) (Payment, error)
	Get(ctx context.Context, paymentID uuid.UUID) (Payment, error)
}

type HTTPPaymentClient struct {
	baseURL    string
	httpClient *http.Client
}

type createPaymentRequest struct {
	OrderID     uuid.UUID `json:"order_id"`
	AmountPaise int64     `json:"amount_paise"`
	Currency    string    `json:"currency"`
}

func NewPaymentClientFromEnv() (PaymentClient, error) {
	baseURL := strings.TrimSpace(os.Getenv("PAYMENT_URL"))
	if baseURL == "" {
		return nil, errors.New("PAYMENT_URL is not set")
	}

	timeout, err := paymentTimeoutFromEnv()
	if err != nil {
		return nil, err
	}

	return &HTTPPaymentClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func paymentTimeoutFromEnv() (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv("PAYMENT_TIMEOUT"))
	if value == "" {
		return defaultPaymentTimeout, nil
	}

	timeout, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse PAYMENT_TIMEOUT: %w", err)
	}
	if timeout <= 0 {
		return 0, errors.New("PAYMENT_TIMEOUT must be greater than zero")
	}
	return timeout, nil
}

func (c *HTTPPaymentClient) Create(
	ctx context.Context,
	orderID uuid.UUID,
	amountPaise int64,
	currency string,
) (Payment, error) {
	body, err := json.Marshal(createPaymentRequest{
		OrderID:     orderID,
		AmountPaise: amountPaise,
		Currency:    currency,
	})
	if err != nil {
		return Payment{}, fmt.Errorf(
			"encode payment request: %v: %w",
			err,
			ErrPaymentInvalidRequest,
		)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/payments",
		bytes.NewReader(body),
	)
	if err != nil {
		return Payment{}, fmt.Errorf(
			"build payment request: %v: %w",
			err,
			ErrPaymentUnavailable,
		)
	}
	request.Header.Set("Content-Type", "application/json")

	return c.do(request)
}

func (c *HTTPPaymentClient) Get(
	ctx context.Context,
	paymentID uuid.UUID,
) (Payment, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL+"/v1/payments/"+paymentID.String(),
		nil,
	)
	if err != nil {
		return Payment{}, fmt.Errorf(
			"build payment lookup request: %v: %w",
			err,
			ErrPaymentUnavailable,
		)
	}

	return c.do(request)
}

func (c *HTTPPaymentClient) do(request *http.Request) (Payment, error) {
	response, err := c.httpClient.Do(request)
	if err != nil {
		return Payment{}, fmt.Errorf(
			"send payment request: %v: %w",
			err,
			ErrPaymentUnavailable,
		)
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK, http.StatusCreated:
		var payment Payment
		if err := json.NewDecoder(response.Body).Decode(&payment); err != nil {
			return Payment{}, fmt.Errorf(
				"decode payment response: %v: %w",
				err,
				ErrPaymentUnavailable,
			)
		}
		return payment, nil
	case http.StatusBadRequest:
		return Payment{}, fmt.Errorf(
			"payment returned status %d: %w",
			response.StatusCode,
			ErrPaymentInvalidRequest,
		)
	case http.StatusNotFound:
		return Payment{}, fmt.Errorf(
			"payment returned status %d: %w",
			response.StatusCode,
			ErrPaymentNotFound,
		)
	case http.StatusConflict:
		return Payment{}, fmt.Errorf(
			"payment returned status %d: %w",
			response.StatusCode,
			ErrPaymentConflict,
		)
	default:
		return Payment{}, fmt.Errorf(
			"payment returned status %d: %w",
			response.StatusCode,
			ErrPaymentUnavailable,
		)
	}
}
