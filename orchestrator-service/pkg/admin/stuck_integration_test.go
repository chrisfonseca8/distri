package admin

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/config"
)

func TestReviveSendsAStuckSagasStepAgainAndOnlyThat(t *testing.T) {
	if os.Getenv("RUN_ADMIN_INTEGRATION") != "1" {
		t.Skip("set RUN_ADMIN_INTEGRATION=1 to run database integration tests")
	}
	db, err := config.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.RequireDedicatedTestDB(db); err != nil {
		t.Skip(err)
	}
	seed := func(id uuid.UUID, status, step string) {
		db.Exec(`INSERT INTO orchestrator.sagas (id, status, current_step, version, deadline_at, created_at, updated_at, total_paise, currency)
			VALUES (?, ?, ?, 3, now() - interval '1 hour', now(), now(), 100, 'INR')`, id.String(), status, step)
		db.Exec(`INSERT INTO orchestrator.saga_steps (saga_id, step, status, attempts, next_attempt_at, routing_key, command_payload, created_at, updated_at)
			VALUES (?, ?, 'RUNNING', 5, now() + interval '1 hour', 'x', '{}'::jsonb, now(), now())`, id.String(), step)
		t.Cleanup(func() { db.Exec(`DELETE FROM orchestrator.sagas WHERE id = ?`, id.String()) })
	}
	stuck, compensating, running := uuid.New(), uuid.New(), uuid.New()
	seed(stuck, "STUCK", "MARK_ORDER_PAID")
	seed(compensating, "STUCK", "RELEASE_STOCK")
	seed(running, "RUNNING", "MARK_ORDER_PAID")

	if err := Revive(context.Background(), db, stuck, 15*time.Minute); err != nil {
		t.Fatalf("revive: %v", err)
	}
	var row struct {
		Status   string
		Version  int64
		Deadline time.Time
		Attempts int
		Due      bool
	}
	db.Raw(`SELECT sg.status, sg.version, sg.deadline_at AS deadline, st.attempts, st.next_attempt_at <= now() + interval '1 second' AS due
		FROM orchestrator.sagas sg JOIN orchestrator.saga_steps st ON st.saga_id = sg.id WHERE sg.id = ?`, stuck.String()).Scan(&row)
	if row.Status != "RUNNING" || row.Version != 4 || row.Attempts != 1 || !row.Due || !row.Deadline.After(time.Now().Add(10*time.Minute)) {
		t.Fatalf("revived saga = %+v, want RUNNING v4, 1 attempt, step due now, deadline about 15 min away", row)
	}

	if err := Revive(context.Background(), db, compensating, time.Minute); err != nil {
		t.Fatalf("revive compensation step: %v", err)
	}
	var status string
	db.Raw(`SELECT status FROM orchestrator.sagas WHERE id = ?`, compensating.String()).Scan(&status)
	if status != "COMPENSATING" {
		t.Fatalf("a stuck undo step must come back as COMPENSATING, got %s", status)
	}

	if err := Revive(context.Background(), db, running, time.Minute); !errors.Is(err, ErrNotRevivable) {
		t.Fatalf("reviving a saga that is not STUCK = %v, want ErrNotRevivable", err)
	}
	if err := Revive(context.Background(), db, stuck, time.Minute); !errors.Is(err, ErrNotRevivable) {
		t.Fatalf("reviving the same saga twice = %v, want ErrNotRevivable (it is RUNNING now)", err)
	}
}
