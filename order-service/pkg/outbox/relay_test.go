package outbox

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
)

type fakeRelayStore struct {
	markedID       uuid.UUID
	rescheduledID  uuid.UUID
	rescheduledIDs []uuid.UUID
	rescheduleWait time.Duration
	lastError      string
}

func (*fakeRelayStore) Claim(
	context.Context,
	string,
	int,
	time.Duration,
) ([]models.Outbox, error) {
	return nil, nil
}

func (store *fakeRelayStore) MarkPublished(
	_ context.Context,
	rowID uuid.UUID,
	_ string,
) (bool, error) {
	store.markedID = rowID
	return true, nil
}

func (store *fakeRelayStore) Reschedule(
	_ context.Context,
	rowID uuid.UUID,
	_ string,
	delay time.Duration,
	lastError string,
) (bool, error) {
	store.rescheduledID = rowID
	store.rescheduledIDs = append(store.rescheduledIDs, rowID)
	store.rescheduleWait = delay
	store.lastError = lastError
	return true, nil
}

type fakePublisher struct {
	err error
}

func (publisher *fakePublisher) Publish(
	context.Context,
	uuid.UUID,
	string,
	[]byte,
) error {
	return publisher.err
}

func TestRelayMarksConfirmedMessagePublished(t *testing.T) {
	store := &fakeRelayStore{}
	relay := &Relay{
		store:     store,
		publisher: &fakePublisher{},
		config:    DefaultRelayConfig(),
		workerID:  "test-worker",
	}
	row := models.Outbox{ID: uuid.New(), MessageID: uuid.New(), Attempts: 1}

	relay.publish(context.Background(), row)

	if store.markedID != row.ID {
		t.Fatalf("marked row = %s, want %s", store.markedID, row.ID)
	}
	if store.rescheduledID != uuid.Nil() {
		t.Fatalf("rescheduled successful row %s", store.rescheduledID)
	}
}

func TestRelayReschedulesFailedPublish(t *testing.T) {
	store := &fakeRelayStore{}
	publishErr := errors.New("broker unavailable")
	relay := &Relay{
		store:     store,
		publisher: &fakePublisher{err: publishErr},
		config:    DefaultRelayConfig(),
		workerID:  "test-worker",
	}
	row := models.Outbox{ID: uuid.New(), MessageID: uuid.New(), Attempts: 3}

	relay.publish(context.Background(), row)

	if store.rescheduledID != row.ID {
		t.Fatalf("rescheduled row = %s, want %s", store.rescheduledID, row.ID)
	}
	if store.rescheduleWait != 4*time.Second {
		t.Fatalf("retry delay = %s, want 4s", store.rescheduleWait)
	}
	if store.lastError != publishErr.Error() {
		t.Fatalf("last error = %q, want %q", store.lastError, publishErr)
	}
	if store.markedID != uuid.Nil() {
		t.Fatalf("marked failed row %s", store.markedID)
	}
}

func TestRetryDelayCapsExponentialBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: time.Second},
		{attempt: 1, want: time.Second},
		{attempt: 2, want: 2 * time.Second},
		{attempt: 3, want: 4 * time.Second},
		{attempt: 6, want: 30 * time.Second},
		{attempt: 20, want: 30 * time.Second},
	}

	for _, test := range tests {
		if got := retryDelay(test.attempt, time.Second, 30*time.Second); got != test.want {
			t.Errorf("attempt %d delay = %s, want %s", test.attempt, got, test.want)
		}
	}
}

func TestRelayReleasesUnpublishedBatchOnShutdown(t *testing.T) {
	store := &fakeRelayStore{}
	relay := &Relay{store: store, workerID: "test-worker"}
	rows := []models.Outbox{
		{ID: uuid.New(), MessageID: uuid.New()},
		{ID: uuid.New(), MessageID: uuid.New()},
	}

	relay.releaseClaimed(rows)

	if len(store.rescheduledIDs) != len(rows) {
		t.Fatalf("released %d rows, want %d", len(store.rescheduledIDs), len(rows))
	}
	for index, row := range rows {
		if store.rescheduledIDs[index] != row.ID {
			t.Errorf("released row %d = %s, want %s", index, store.rescheduledIDs[index], row.ID)
		}
	}
}

func TestRelayConfigRequiresLeaseForWholeBatch(t *testing.T) {
	config := DefaultRelayConfig()
	config.Lease = time.Duration(config.BatchSize) * config.PublishTimeout

	if err := config.Validate(); err == nil {
		t.Fatal("Validate() accepted a lease that can expire before the batch finishes")
	}
}
