package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/inventory-service/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidReservationInput = errors.New("invalid reservation input")
	ErrReservationConflict     = errors.New("order was used for a different reservation request")
	ErrInsufficientStock       = errors.New("insufficient stock")
)

const (
	maxReservationLines = 20
	maxQuantityPerLine  = 100
)

type ReservationItemInput struct {
	ProductID uuid.UUID `json:"product_id"`
	Quantity  int64     `json:"quantity"`
}

type CreateReservationInput struct {
	OrderID     uuid.UUID              `json:"order_id"`
	RequestHash string                 `json:"request_hash"`
	Items       []ReservationItemInput `json:"items"`
}

type ReservedItem struct {
	ProductID      uuid.UUID `json:"product_id"`
	Quantity       int64     `json:"quantity"`
	UnitPricePaise int64     `json:"unit_price_paise"`
	Currency       string    `json:"currency"`
}

type CreateReservationResult struct {
	ReservationID uuid.UUID      `json:"reservation_id"`
	Status        string         `json:"status"`
	TotalPaise    int64          `json:"total_paise"`
	Currency      string         `json:"currency"`
	Items         []ReservedItem `json:"items"`
	Created       bool           `json:"-"`
}

type ReservationService struct {
	db *gorm.DB
}

func NewReservationService(db *gorm.DB) *ReservationService {
	return &ReservationService{db: db}
}

func (s *ReservationService) CreateReservation(
	ctx context.Context,
	input CreateReservationInput,
) (CreateReservationResult, error) {
	items, err := prepareReservationItems(input)
	if err != nil {
		return CreateReservationResult{}, err
	}

	var result CreateReservationResult
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		reservation, created, err := claimReservation(tx, input)
		if err != nil {
			return err
		}
		if !created {
			result, err = loadReservationResult(tx, reservation)
			result.Created = created
			return err
		}

		products, err := loadProducts(tx, items)
		if err != nil {
			return err
		}
		if err := reserveInventory(tx, items); err != nil {
			return err
		}
		if err := createReservationItems(tx, reservation.ID, items, products); err != nil {
			return err
		}
		if err := markReservationReserved(tx, reservation.ID); err != nil {
			return err
		}

		reservation.Status = "RESERVED"
		result, err = loadReservationResult(tx, reservation)
		result.Created = created
		return err
	})

	return result, err
}

func prepareReservationItems(input CreateReservationInput) ([]ReservationItemInput, error) {
	if input.OrderID == uuid.Nil() || strings.TrimSpace(input.RequestHash) == "" {
		return nil, ErrInvalidReservationInput
	}
	if len(input.Items) == 0 || len(input.Items) > maxReservationLines {
		return nil, ErrInvalidReservationInput
	}

	items := slices.Clone(input.Items)
	slices.SortFunc(items, func(left, right ReservationItemInput) int {
		return strings.Compare(left.ProductID.String(), right.ProductID.String())
	})
	for index, item := range items {
		if item.ProductID == uuid.Nil() || item.Quantity <= 0 || item.Quantity > maxQuantityPerLine {
			return nil, ErrInvalidReservationInput
		}
		if index > 0 && item.ProductID == items[index-1].ProductID {
			return nil, ErrInvalidReservationInput
		}
	}
	return items, nil
}

func claimReservation(
	tx *gorm.DB,
	input CreateReservationInput,
) (models.Reservation, bool, error) {
	reservation := models.Reservation{
		OrderID:     input.OrderID,
		RequestHash: input.RequestHash,
		Status:      "PENDING",
	}
	insert := tx.Table("inventory.reservations").
		Omit(clause.Associations).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "order_id"}},
			DoNothing: true,
		}).
		Create(&reservation)
	if insert.Error != nil {
		return models.Reservation{}, false, fmt.Errorf("create reservation: %w", insert.Error)
	}
	if insert.RowsAffected == 1 {
		return reservation, true, nil
	}

	if err := tx.Table("inventory.reservations").
		Where("order_id = ?", input.OrderID).
		First(&reservation).Error; err != nil {
		return models.Reservation{}, false, fmt.Errorf("get reservation: %w", err)
	}
	if reservation.RequestHash != input.RequestHash {
		return models.Reservation{}, false, ErrReservationConflict
	}
	return reservation, false, nil
}

func loadProducts(
	tx *gorm.DB,
	items []ReservationItemInput,
) (map[uuid.UUID]models.Product, error) {
	productIDs := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		productIDs = append(productIDs, item.ProductID)
	}

	var productRows []models.Product
	if err := tx.Table("inventory.products").Where("id IN ?", productIDs).Find(&productRows).Error; err != nil {
		return nil, fmt.Errorf("load products: %w", err)
	}

	products := make(map[uuid.UUID]models.Product, len(productRows))
	for _, product := range productRows {
		products[product.ID] = product
	}
	for _, item := range items {
		if _, found := products[item.ProductID]; !found {
			return nil, fmt.Errorf("%w: %s", ErrProductNotFound, item.ProductID)
		}
	}
	return products, nil
}

func reserveInventory(tx *gorm.DB, items []ReservationItemInput) error {
	now := time.Now()
	for _, item := range items {
		update := tx.Table("inventory.inventories").
			Where(
				"product_id = ? AND on_hand - reserved - allocated >= ?",
				item.ProductID,
				item.Quantity,
			).
			Updates(map[string]any{
				"reserved":   gorm.Expr("reserved + ?", item.Quantity),
				"updated_at": now,
			})
		if update.Error != nil {
			return fmt.Errorf("reserve product %s: %w", item.ProductID, update.Error)
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("%w for product %s", ErrInsufficientStock, item.ProductID)
		}
	}
	return nil
}

func createReservationItems(
	tx *gorm.DB,
	reservationID uuid.UUID,
	items []ReservationItemInput,
	products map[uuid.UUID]models.Product,
) error {
	rows := make([]models.ReservationItem, 0, len(items))
	for _, item := range items {
		product := products[item.ProductID]
		rows = append(rows, models.ReservationItem{
			ReservationID:  reservationID,
			ProductID:      item.ProductID,
			Quantity:       item.Quantity,
			UnitPricePaise: product.PricePaise,
			Currency:       product.Currency,
		})
	}
	if err := tx.Table("inventory.reservation_items").
		Omit(clause.Associations).
		Create(&rows).Error; err != nil {
		return fmt.Errorf("create reservation items: %w", err)
	}
	return nil
}

func markReservationReserved(tx *gorm.DB, reservationID uuid.UUID) error {
	update := tx.Table("inventory.reservations").
		Where("id = ? AND status = ?", reservationID, "PENDING").
		Updates(map[string]any{"status": "RESERVED", "updated_at": time.Now()})
	if update.Error != nil {
		return fmt.Errorf("mark reservation reserved: %w", update.Error)
	}
	if update.RowsAffected != 1 {
		return errors.New("reservation was not marked reserved")
	}
	return nil
}

func loadReservationResult(
	tx *gorm.DB,
	reservation models.Reservation,
) (CreateReservationResult, error) {
	var rows []models.ReservationItem
	if err := tx.Table("inventory.reservation_items").
		Where("reservation_id = ?", reservation.ID).
		Order("product_id").
		Find(&rows).Error; err != nil {
		return CreateReservationResult{}, fmt.Errorf("load reservation items: %w", err)
	}

	result := CreateReservationResult{
		ReservationID: reservation.ID,
		Status:        reservation.Status,
		Items:         make([]ReservedItem, 0, len(rows)),
	}
	for _, row := range rows {
		if result.Currency == "" {
			result.Currency = row.Currency
		}
		if row.Currency != result.Currency {
			return CreateReservationResult{}, errors.New("reservation contains multiple currencies")
		}
		if row.UnitPricePaise <= 0 || row.Quantity > math.MaxInt64/row.UnitPricePaise {
			return CreateReservationResult{}, errors.New("reservation total exceeds BIGINT range")
		}
		lineTotal := row.Quantity * row.UnitPricePaise
		if result.TotalPaise > math.MaxInt64-lineTotal {
			return CreateReservationResult{}, errors.New("reservation total exceeds BIGINT range")
		}
		result.TotalPaise += lineTotal
		result.Items = append(result.Items, ReservedItem{
			ProductID:      row.ProductID,
			Quantity:       row.Quantity,
			UnitPricePaise: row.UnitPricePaise,
			Currency:       row.Currency,
		})
	}
	return result, nil
}
