package reply

import (
	"errors"
	"fmt"
	"testing"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/messages"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/messaging"
)

type testSQLStateError struct {
	state string
}

func (e testSQLStateError) Error() string    { return "database error" }
func (e testSQLStateError) SQLState() string { return e.state }

func TestHandlerClassifiesMalformedMessagesAsPermanent(t *testing.T) {
	handler := NewHandler(nil)
	validBody := fmt.Sprintf(
		`{"order_id":%q,"step":"CREATE_ORDER","outcome":"Succeeded"}`,
		uuid.New().String(),
	)
	tests := []messaging.Delivery{
		{Body: []byte(`not-json`), MessageId: "message-id"},
		{Body: []byte(`{}`), MessageId: "message-id"},
		{Body: []byte(validBody)},
	}

	for _, delivery := range tests {
		err := handler.Handle(t.Context(), delivery)
		var permanent *messaging.PermanentError
		if !errors.As(err, &permanent) {
			t.Fatalf("error = %v, want PermanentError", err)
		}
	}
}

func TestFinishedStepStatus(t *testing.T) {
	tests := []struct {
		outcome messages.Outcome
		want    string
	}{
		{outcome: messages.Succeeded, want: "SUCCEEDED"},
		{outcome: messages.Failed, want: "FAILED"},
		{outcome: messages.Ambiguous, want: "AMBIGUOUS"},
	}
	for _, test := range tests {
		got, err := finishedStepStatus(test.outcome)
		if err != nil {
			t.Fatalf("finishedStepStatus(%q): %v", test.outcome, err)
		}
		if got != test.want {
			t.Fatalf("finishedStepStatus(%q) = %q, want %q", test.outcome, got, test.want)
		}
	}
	if _, err := finishedStepStatus(messages.Outcome("unknown")); err == nil {
		t.Fatal("finishedStepStatus accepted an unknown outcome")
	}
}

func TestIsPermanentDatabaseError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "data exception", err: testSQLStateError{state: "22003"}, want: true},
		{name: "integrity violation", err: testSQLStateError{state: "23503"}, want: true},
		{name: "wrapped integrity violation", err: fmt.Errorf("insert: %w", testSQLStateError{state: "23514"}), want: true},
		{name: "connection exception", err: testSQLStateError{state: "08006"}, want: false},
		{name: "serialization failure", err: testSQLStateError{state: "40001"}, want: false},
		{name: "non database error", err: errors.New("context canceled"), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isPermanentDatabaseError(test.err); got != test.want {
				t.Fatalf("isPermanentDatabaseError() = %t, want %t", got, test.want)
			}
		})
	}
}
