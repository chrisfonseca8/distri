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

const defaultInventoryTimeout = 3 * time.Second

var (
	ErrInvalidRequest       = errors.New("inventory rejected the request")
	ErrProductNotFound      = errors.New("inventory product not found")
	ErrInsufficientStock    = errors.New("insufficient inventory stock")
	ErrInventoryUnavailable = errors.New("inventory service unavailable")
)

type ReserveItem struct {
	ProductID uuid.UUID `json:"product_id"`
	Quantity  int64     `json:"quantity"`
}

type ReservedLine struct {
	ProductID      uuid.UUID `json:"product_id"`
	Quantity       int64     `json:"quantity"`
	UnitPricePaise int64     `json:"unit_price_paise"`
	Currency       string    `json:"currency"`
}

type ReserveResult struct {
	ReservationID uuid.UUID      `json:"reservation_id"`
	TotalPaise    int64          `json:"total_paise"`
	Currency      string         `json:"currency"`
	Items         []ReservedLine `json:"items"`
}

type InventoryClient interface {
	Reserve(
		ctx context.Context,
		orderID uuid.UUID,
		requestHash string,
		items []ReserveItem,
	) (ReserveResult, error)
}

type HTTPInventoryClient struct {
	baseURL    string
	httpClient *http.Client
}

type reserveRequest struct {
	OrderID     uuid.UUID     `json:"order_id"`
	RequestHash string        `json:"request_hash"`
	Items       []ReserveItem `json:"items"`
}

func NewInventoryClientFromEnv() (InventoryClient, error) {
	baseURL := strings.TrimSpace(os.Getenv("INVENTORY_URL"))
	if baseURL == "" {
		return nil, errors.New("INVENTORY_URL is not set")
	}

	timeout, err := inventoryTimeoutFromEnv()
	if err != nil {
		return nil, err
	}

	return &HTTPInventoryClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func inventoryTimeoutFromEnv() (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv("INVENTORY_TIMEOUT"))
	if value == "" {
		return defaultInventoryTimeout, nil
	}

	timeout, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse INVENTORY_TIMEOUT: %w", err)
	}
	if timeout <= 0 {
		return 0, errors.New("INVENTORY_TIMEOUT must be greater than zero")
	}
	return timeout, nil
}

func (c *HTTPInventoryClient) Reserve(
	ctx context.Context,
	orderID uuid.UUID,
	requestHash string,
	items []ReserveItem,
) (ReserveResult, error) {
	body, err := json.Marshal(reserveRequest{
		OrderID:     orderID,
		RequestHash: requestHash,
		Items:       items,
	})
	if err != nil {
		return ReserveResult{}, fmt.Errorf(
			"encode inventory reservation request: %v: %w",
			err,
			ErrInvalidRequest,
		)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/internal/v1/reservations",
		bytes.NewReader(body),
	)
	if err != nil {
		return ReserveResult{}, fmt.Errorf(
			"build inventory reservation request: %v: %w",
			err,
			ErrInventoryUnavailable,
		)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return ReserveResult{}, fmt.Errorf(
			"send inventory reservation request: %v: %w",
			err,
			ErrInventoryUnavailable,
		)
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK, http.StatusCreated:
		var result ReserveResult
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			return ReserveResult{}, fmt.Errorf(
				"decode inventory reservation response: %v: %w",
				err,
				ErrInventoryUnavailable,
			)
		}
		return result, nil
	case http.StatusBadRequest:
		return ReserveResult{}, fmt.Errorf(
			"inventory returned status %d: %w",
			response.StatusCode,
			ErrInvalidRequest,
		)
	case http.StatusNotFound:
		return ReserveResult{}, fmt.Errorf(
			"inventory returned status %d: %w",
			response.StatusCode,
			ErrProductNotFound,
		)
	case http.StatusConflict:
		return ReserveResult{}, fmt.Errorf(
			"inventory returned status %d: %w",
			response.StatusCode,
			ErrInsufficientStock,
		)
	default:
		return ReserveResult{}, fmt.Errorf(
			"inventory returned status %d: %w",
			response.StatusCode,
			ErrInventoryUnavailable,
		)
	}
}
