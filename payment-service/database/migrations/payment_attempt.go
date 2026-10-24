package migrations

import (
	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"gorm.io/gorm"
)

// MigratePaymentAttempts creates or updates every payment table.
func MigratePaymentAttempts(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&models.PaymentAttempt{},
		&models.Job{},
		&models.Outbox{},
		&models.ProcessedMessage{},
		&models.WebhookEvent{},
		&models.CompensationRecord{},
		&models.CompensationEvent{},
	); err != nil {
		return err
	}

	// AutoMigrate never alters an existing CHECK constraint, so the status
	// constraint is replaced explicitly when AWAITING_PAYMENT is introduced.
	if err := db.Exec(`ALTER TABLE payments.payment_attempts
		DROP CONSTRAINT IF EXISTS chk_payment_attempts_status`).Error; err != nil {
		return err
	}
	if err := db.Exec(`ALTER TABLE payments.payment_attempts
		ADD CONSTRAINT chk_payment_attempts_status CHECK (status IN
		('CREATED','PENDING','AWAITING_PAYMENT','SUCCEEDED','FAILED','UNKNOWN'))`).Error; err != nil {
		return err
	}

	if err := migrateCompensationLifecycle(db); err != nil {
		return err
	}

	// Result notification was deliberately removed from the C6a scope. These
	// rows never had a handler, so retaining them would leave a false backlog.
	return db.Table("payments.jobs").
		Where("type = ?", "notify_order").
		Delete(&models.Job{}).Error
}

// migrateCompensationLifecycle moves compensation records from the Stage 4 model
// (REQUIRED / REQUESTED / REFUND_FAILED / REVIEW, unique per payment id) to the
// lifecycle model. It is idempotent. Original statuses are preserved in
// payments.compensation_events before they are rewritten.
func migrateCompensationLifecycle(db *gorm.DB) error {
	statements := []string{
		// the old per-payment-id unique index is replaced by (gateway, payment id)
		`DROP INDEX IF EXISTS payments.uidx_compensation_gateway_payment`,
		`ALTER TABLE payments.compensation_records DROP CONSTRAINT IF EXISTS chk_compensation_status`,
		// the environment label of each record comes from the attempt that owns the money
		`UPDATE payments.compensation_records c SET gateway = a.gateway
			FROM payments.payment_attempts a WHERE a.id = c.payment_attempt_id AND c.gateway = ''`,
		// Stage 4 REQUESTED: the gateway accepted a request but its status was never
		// checked, so it is PENDING (unverified) until a fetch says otherwise.
		`INSERT INTO payments.compensation_events (record_id, from_status, to_status, detail)
			SELECT id, status, 'PENDING', 'legacy REQUESTED: the gateway accepted the request earlier; its provider status was never checked, so it is pending until reconciled'
			FROM payments.compensation_records WHERE status = 'REQUESTED'`,
		`UPDATE payments.compensation_records SET status = 'PENDING', next_attempt_at = now() WHERE status = 'REQUESTED'`,
		// Stage 4 REFUND_FAILED rows were produced by a refund call routed to the real
		// API by mistake: the original failure is kept in the history, and the record is
		// parked for review. It is not marked refunded.
		`INSERT INTO payments.compensation_events (record_id, from_status, to_status, detail)
			SELECT id, status, 'REVIEW', 'legacy REFUND_FAILED: the refund call was rejected after being routed to the real Razorpay API by an environment mistake (2026-10-24); original failure preserved here. Not refunded.'
			FROM payments.compensation_records WHERE status = 'REFUND_FAILED'`,
		`UPDATE payments.compensation_records SET status = 'REVIEW',
			review_reason = 'legacy REFUND_FAILED from an environment mistake; the simulator-side state could not be established; not refunded'
			WHERE status = 'REFUND_FAILED'`,
		`ALTER TABLE payments.compensation_records ADD CONSTRAINT chk_compensation_status CHECK (status IN
			('REQUIRED','SUBMITTING','UNKNOWN','PENDING','PROCESSED','FAILED','REVIEW'))`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}
