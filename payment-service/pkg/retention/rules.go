package retention

import "time"

func messages(c Config) time.Duration { return c.Messages }
func parked(c Config) time.Duration   { return c.Parked }

// Rules returns the payment service's purge rules: message bookkeeping, finished jobs
// and webhook events that are done. Payment attempts and compensation records and
// their history are financial records and are never purged here. Dead jobs stay for
// a person to look at.
func Rules() []Rule {
	return []Rule{
		{"outbox", messages, `DELETE FROM payments.outbox WHERE id IN (
			SELECT id FROM payments.outbox WHERE published_at IS NOT NULL AND published_at < ? ORDER BY published_at LIMIT ?)`},
		{"processed_messages", messages, `DELETE FROM payments.processed_messages WHERE message_id IN (
			SELECT message_id FROM payments.processed_messages WHERE processed_at < ? ORDER BY processed_at LIMIT ?)`},
		{"jobs", messages, `DELETE FROM payments.jobs WHERE id IN (
			SELECT id FROM payments.jobs WHERE status = 'succeeded' AND updated_at < ? ORDER BY updated_at LIMIT ?)`},
		{"webhook_events_done", messages, `DELETE FROM payments.webhook_events WHERE id IN (
			SELECT id FROM payments.webhook_events WHERE status IN ('PROCESSED', 'IGNORED') AND received_at < ? ORDER BY received_at LIMIT ?)`},
		{"webhook_events_parked", parked, `DELETE FROM payments.webhook_events WHERE id IN (
			SELECT id FROM payments.webhook_events WHERE status = 'PARKED' AND received_at < ? ORDER BY received_at LIMIT ?)`},
	}
}
