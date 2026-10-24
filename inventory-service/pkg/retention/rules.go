package retention

import "time"

func messages(c Config) time.Duration { return c.Messages }

// Rules returns this service's purge rules: message bookkeeping only. Orders, order
// items, reservations and stock are business records and are never purged here.
func Rules() []Rule {
	return []Rule{
		{"outbox", messages, `DELETE FROM inventory.outbox WHERE id IN (
			SELECT id FROM inventory.outbox WHERE published_at IS NOT NULL AND published_at < ? ORDER BY published_at LIMIT ?)`},
		{"processed_messages", messages, `DELETE FROM inventory.processed_messages WHERE message_id IN (
			SELECT message_id FROM inventory.processed_messages WHERE processed_at < ? ORDER BY processed_at LIMIT ?)`},
	}
}
