package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"uuid"
)

const defaultInventoryTimeout = 3 * time.Second

var (
	ErrInvalidRequest         = errors.New("inventory rejected the request")
	ErrProductNotFound        = errors.New("inventory product not found")
	ErrInsufficientStock      = errors.New("insufficient inventory stock")
	ErrReservationNotReserved = errors.New("inventory reservation is not reserved")
	ErrReservationNotFound    = errors.New("inventory reservation not found")
	ErrReservationConflict    = errors.New("inventory reservation status conflict")
	ErrInventoryUnavailable   = errors.New("inventory service unavailable")
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
	Status        string         `json:"status"`
	TotalPaise    int64          `json:"total_paise"`
	Currency      string         `json:"currency"`
	Items         []ReservedLine `json:"items"`
}

type ChangeReservationResult struct {
	ReservationID uuid.UUID `json:"reservation_id"`
	OrderID       uuid.UUID `json:"order_id"`
	Status        string    `json:"status"`
}

type ReservationSummary struct {
	ReservationID uuid.UUID `json:"reservation_id"`
	OrderID       uuid.UUID `json:"order_id"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

type ReservationCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// ReservationClient is the slice of inventory the reservation sweeper needs.
type ReservationClient interface {
	ListReserved(ctx context.Context, olderThan time.Time, after *ReservationCursor, limit int) ([]ReservationSummary, error)
	Release(ctx context.Context, orderID uuid.UUID) (ChangeReservationResult, error)
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

// NewInventoryClientFromEnv builds the inventory client from environment variables.
func NewInventoryClientFromEnv() (InventoryClient, error) {
	client, err := NewHTTPInventoryClientFromEnv()
	if err != nil {
		return nil, err
	}
	return client, nil
}

// NewHTTPInventoryClientFromEnv reads INVENTORY_URL and INVENTORY_TIMEOUT and returns the HTTP client.
func NewHTTPInventoryClientFromEnv() (*HTTPInventoryClient, error) {
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

// inventoryTimeoutFromEnv reads INVENTORY_TIMEOUT, falling back to the default when it is unset.
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

// Reserve asks inventory-service to reserve stock for an order; it is idempotent on orderID.
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
		if result.Status != "RESERVED" {
			return ReserveResult{}, fmt.Errorf(
				"inventory returned reservation status %q: %w",
				result.Status,
				ErrReservationNotReserved,
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

// ListReserved returns one page of reservations older than olderThan that are still reserved.
func (c *HTTPInventoryClient) ListReserved(
	ctx context.Context,
	olderThan time.Time,
	after *ReservationCursor,
	limit int,
) ([]ReservationSummary, error) {
	query := url.Values{}
	query.Set("status", "RESERVED")
	query.Set("older_than", olderThan.UTC().Format(time.RFC3339Nano))
	query.Set("limit", strconv.Itoa(limit))
	if after != nil {
		query.Set("after_created_at", after.CreatedAt.UTC().Format(time.RFC3339Nano))
		query.Set("after_id", after.ID.String())
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL+"/internal/v1/reservations?"+query.Encode(),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"build inventory reservation list request: %v: %w",
			err,
			ErrInventoryUnavailable,
		)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf(
			"send inventory reservation list request: %v: %w",
			err,
			ErrInventoryUnavailable,
		)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"inventory reservation list returned status %d: %w",
			response.StatusCode,
			ErrInventoryUnavailable,
		)
	}

	var reservations []ReservationSummary
	if err := json.NewDecoder(response.Body).Decode(&reservations); err != nil {
		return nil, fmt.Errorf(
			"decode inventory reservation list response: %v: %w",
			err,
			ErrInventoryUnavailable,
		)
	}
	return reservations, nil
}

// Release asks inventory-service to give a reservation's stock back.
func (c *HTTPInventoryClient) Release(
	ctx context.Context,
	orderID uuid.UUID,
) (ChangeReservationResult, error) {
	return c.changeReservation(ctx, orderID, "release")
}

// changeReservation calls the confirm or release endpoint for an order and decodes the result.
func (c *HTTPInventoryClient) changeReservation(
	ctx context.Context,
	orderID uuid.UUID,
	action string,
) (ChangeReservationResult, error) {
	url := fmt.Sprintf(
		"%s/internal/v1/reservations/%s/%s",
		c.baseURL,
		orderID,
		action,
	)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return ChangeReservationResult{}, fmt.Errorf(
			"build inventory %s request: %v: %w",
			action,
			err,
			ErrInventoryUnavailable,
		)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return ChangeReservationResult{}, fmt.Errorf(
			"send inventory %s request: %v: %w",
			action,
			err,
			ErrInventoryUnavailable,
		)
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK, http.StatusCreated:
		var result ChangeReservationResult
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			return ChangeReservationResult{}, fmt.Errorf(
				"decode inventory %s response: %v: %w",
				action,
				err,
				ErrInventoryUnavailable,
			)
		}
		return result, nil
	case http.StatusBadRequest:
		return ChangeReservationResult{}, fmt.Errorf(
			"inventory %s returned status %d: %w",
			action,
			response.StatusCode,
			ErrInvalidRequest,
		)
	case http.StatusNotFound:
		return ChangeReservationResult{}, fmt.Errorf(
			"inventory %s returned status %d: %w",
			action,
			response.StatusCode,
			ErrReservationNotFound,
		)
	case http.StatusConflict:
		return ChangeReservationResult{}, fmt.Errorf(
			"inventory %s returned status %d: %w",
			action,
			response.StatusCode,
			ErrReservationConflict,
		)
	default:
		return ChangeReservationResult{}, fmt.Errorf(
			"inventory %s returned status %d: %w",
			action,
			response.StatusCode,
			ErrInventoryUnavailable,
		)
	}
}
