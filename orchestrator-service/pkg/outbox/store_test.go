package outbox

import (
	"encoding/json"
	"testing"
	"uuid"

	"gorm.io/gorm"
)

func TestEnqueueValidatesMessage(t *testing.T) {
	tests := []struct {
		name    string
		message Message
	}{
		{
			name: "missing message ID",
			message: Message{
				RoutingKey: "order.create",
				Payload:    json.RawMessage(`{}`),
			},
		},
		{
			name: "missing routing key",
			message: Message{
				MessageID: uuid.New(),
				Payload:   json.RawMessage(`{}`),
			},
		},
		{
			name: "invalid payload",
			message: Message{
				MessageID:  uuid.New(),
				RoutingKey: "order.create",
				Payload:    json.RawMessage(`not-json`),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := Enqueue(&gorm.DB{}, test.message); err == nil {
				t.Fatal("Enqueue() accepted invalid input")
			}
		})
	}
}
