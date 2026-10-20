package sweeper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/pkg/clients"
	"github.com/chrisfonseca08/distri/order-service/pkg/config"
	"github.com/chrisfonseca08/distri/order-service/pkg/services"
	"github.com/chrisfonseca08/distri/order-service/pkg/utils/state"
)

const maxSweepPages = 100

type inventoryClient interface {
	ListReserved(context.Context, time.Time, *clients.ReservationCursor, int) ([]clients.ReservationSummary, error)
	Release(context.Context, uuid.UUID) (clients.ChangeReservationResult, error)
}

type orderStatusReader interface {
	GetOrderStatus(context.Context, uuid.UUID) (state.OrderStatus, error)
}

type ReservationSweeper struct {
	inventory inventoryClient
	orders    orderStatusReader
	config    config.SweeperConfig
	now       func() time.Time
}

func NewReservationSweeper(
	inventory inventoryClient,
	orders orderStatusReader,
	sweeperConfig config.SweeperConfig,
) (*ReservationSweeper, error) {
	if err := sweeperConfig.Validate(); err != nil {
		return nil, fmt.Errorf("validate reservation sweeper configuration: %w", err)
	}
	return &ReservationSweeper{
		inventory: inventory,
		orders:    orders,
		config:    sweeperConfig,
		now:       time.Now,
	}, nil
}

func (s *ReservationSweeper) Run(ctx context.Context) {
	slog.Info(
		"reservation sweeper started",
		"ttl", s.config.ReservationTTL,
		"interval", s.config.Interval,
		"batch_size", s.config.BatchSize,
	)
	defer slog.Info("reservation sweeper stopped")

	s.sweepAndLog(ctx)
	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepAndLog(ctx)
		}
	}
}

func (s *ReservationSweeper) sweepAndLog(ctx context.Context) {
	if err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
		slog.Error("sweep expired reservations", "error", err)
	}
}

func (s *ReservationSweeper) Sweep(ctx context.Context) error {
	cutoff := s.now().UTC().Add(-s.config.ReservationTTL)
	var sweepErrors []error
	var cursor *clients.ReservationCursor
	for page := 0; page < maxSweepPages; page++ {
		reservations, err := s.inventory.ListReserved(ctx, cutoff, cursor, s.config.BatchSize)
		if err != nil {
			return errors.Join(append(
				sweepErrors,
				fmt.Errorf("list expired inventory reservations: %w", err),
			)...)
		}

		for _, reservation := range reservations {
			if ctx.Err() != nil {
				return errors.Join(append(sweepErrors, ctx.Err())...)
			}

			orderStatus, lookupErr := s.orders.GetOrderStatus(ctx, reservation.OrderID)
			switch {
			case errors.Is(lookupErr, services.ErrOrderNotFound):
				if err := s.release(ctx, reservation, "orphan"); err != nil {
					sweepErrors = append(sweepErrors, err)
				}
			case lookupErr != nil:
				sweepErrors = append(sweepErrors, fmt.Errorf(
					"look up order %s for reservation %s: %w",
					reservation.OrderID,
					reservation.ReservationID,
					lookupErr,
				))
			case orderStatus == state.OrderCancelled || orderStatus == state.OrderExpired:
				if err := s.release(ctx, reservation, string(orderStatus)); err != nil {
					sweepErrors = append(sweepErrors, err)
				}
			case orderStatus == state.OrderPaid:
				slog.Warn(
					"paid order still has a reserved inventory reservation; leaving it for settlement",
					"order_id", reservation.OrderID.String(),
					"reservation_id", reservation.ReservationID.String(),
				)
			case orderStatus == state.OrderPendingPayment:
				// Time alone cannot prove that payment failed, so the reservation stays held.
			default:
				sweepErrors = append(sweepErrors, fmt.Errorf(
					"order %s has unknown status %q; reservation %s was not released",
					reservation.OrderID,
					orderStatus,
					reservation.ReservationID,
				))
			}
		}

		if len(reservations) < s.config.BatchSize {
			return errors.Join(sweepErrors...)
		}
		last := reservations[len(reservations)-1]
		cursor = &clients.ReservationCursor{CreatedAt: last.CreatedAt, ID: last.ReservationID}
	}
	return errors.Join(append(
		sweepErrors,
		fmt.Errorf("reservation sweep stopped after page cap of %d", maxSweepPages),
	)...)
}

func (s *ReservationSweeper) release(
	ctx context.Context,
	reservation clients.ReservationSummary,
	reason string,
) error {
	result, err := s.inventory.Release(ctx, reservation.OrderID)
	if err != nil {
		return fmt.Errorf(
			"release reservation %s for order %s (%s): %w",
			reservation.ReservationID,
			reservation.OrderID,
			reason,
			err,
		)
	}
	if result.Status != "RELEASED" {
		return fmt.Errorf(
			"release reservation %s for order %s (%s): inventory returned status %q",
			reservation.ReservationID,
			reservation.OrderID,
			reason,
			result.Status,
		)
	}
	slog.Info(
		"released expired inventory reservation",
		"order_id", reservation.OrderID.String(),
		"reservation_id", reservation.ReservationID.String(),
		"reason", reason,
	)
	return nil
}
