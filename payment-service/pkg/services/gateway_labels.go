package services

import (
	"context"
	"fmt"
	"log/slog"

	"gorm.io/gorm"
)

// knownGatewayLabels are the environment labels durable work may carry: "sim" (the
// in-process simulator), "razorpay" (the real API) and "razorpay-sim" (the gateway
// simulator behind the Razorpay adapter).
var knownGatewayLabels = map[string]bool{"sim": true, "razorpay": true, "razorpay-sim": true}

// ValidateGatewayLabels is the startup check that keeps environments apart. It
// refuses to start when unsettled work carries a label this build does not know (so
// it could not be routed safely), and it reports, without touching it, unsettled work
// that belongs to other environments: that work stays untouched until a process of
// the right environment runs.
func ValidateGatewayLabels(ctx context.Context, db *gorm.DB, own string) error {
	if !knownGatewayLabels[own] {
		return fmt.Errorf("gateway environment %q is not one of sim, razorpay, razorpay-sim", own)
	}
	var rows []struct {
		Source  string
		Gateway string
		Count   int64
	}
	err := db.WithContext(ctx).Raw(`
		SELECT 'payment_attempts' AS source, gateway, count(*) AS count
		FROM payments.payment_attempts
		WHERE status IN ('CREATED','PENDING','AWAITING_PAYMENT','UNKNOWN')
		GROUP BY gateway
		UNION ALL
		SELECT 'compensation_records', gateway, count(*)
		FROM payments.compensation_records
		WHERE status NOT IN ('PROCESSED','REVIEW')
		GROUP BY gateway`).Scan(&rows).Error
	if err != nil {
		return fmt.Errorf("check gateway labels: %w", err)
	}
	for _, row := range rows {
		switch {
		case !knownGatewayLabels[row.Gateway]:
			return fmt.Errorf("%d unsettled %s rows carry the unknown gateway label %q; refusing to start", row.Count, row.Source, row.Gateway)
		case row.Gateway != own:
			slog.Warn("unsettled work belongs to another gateway environment and will not be touched by this process",
				"source", row.Source, "gateway", row.Gateway, "this_process", own, "rows", row.Count)
		}
	}
	return nil
}
