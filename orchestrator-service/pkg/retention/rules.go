package retention

import "time"

func messages(c Config) time.Duration  { return c.Messages }
func checkouts(c Config) time.Duration { return c.Checkouts }

// Rules returns the orchestrator's purge rules. Finished sagas are deleted with their
// steps (ON DELETE CASCADE); sagas that are RUNNING, COMPENSATING or STUCK never are.
func Rules() []Rule {
	return []Rule{
		{"outbox", messages, `DELETE FROM orchestrator.outbox WHERE id IN (
			SELECT id FROM orchestrator.outbox WHERE published_at IS NOT NULL AND published_at < ? ORDER BY published_at LIMIT ?)`},
		{"processed_messages", messages, `DELETE FROM orchestrator.processed_messages WHERE message_id IN (
			SELECT message_id FROM orchestrator.processed_messages WHERE processed_at < ? ORDER BY processed_at LIMIT ?)`},
		{"checkout_attempts", checkouts, `DELETE FROM orchestrator.checkout_attempts WHERE idempotency_key IN (
			SELECT idempotency_key FROM orchestrator.checkout_attempts WHERE status = 'completed' AND updated_at < ? ORDER BY updated_at LIMIT ?)`},
		{"sagas", checkouts, `DELETE FROM orchestrator.sagas WHERE id IN (
			SELECT id FROM orchestrator.sagas WHERE status IN ('COMPLETED', 'COMPENSATED') AND updated_at < ? ORDER BY updated_at LIMIT ?)`},
	}
}
