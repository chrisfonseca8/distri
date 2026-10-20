package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"github.com/chrisfonseca08/distri/order-service/pkg/config"
	"gorm.io/datatypes"
)

func TestClaimConcurrentWorkers(t *testing.T) {
	if os.Getenv("RUN_JOBS_INTEGRATION") != "1" {
		t.Skip("set RUN_JOBS_INTEGRATION=1 to run database integration tests")
	}

	db, err := config.OpenDB()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	const jobCount = 10
	jobs := make([]models.Job, 0, jobCount)
	ids := make([]uuid.UUID, 0, jobCount)
	for range jobCount {
		id := uuid.New()
		ids = append(ids, id)
		jobs = append(jobs, models.Job{
			ID:      id,
			Type:    "claim_integration_test",
			OrderID: uuid.New(),
		})
	}
	if err := db.Table("orders.jobs").Create(&jobs).Error; err != nil {
		t.Fatalf("insert jobs: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Table("orders.jobs").Where("id IN ?", ids).Delete(&models.Job{}).Error; err != nil {
			t.Errorf("delete test jobs: %v", err)
		}
	})

	store := NewStore(db)
	results := make([][]models.Job, 2)
	errors := make([]error, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := range results {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			<-start
			results[i], errors[i] = store.Claim(
				context.Background(),
				"claim-test-worker-"+string(rune('A'+i)),
				jobCount,
				time.Minute,
			)
		}(i)
	}
	close(start)
	workers.Wait()

	claimed := make(map[uuid.UUID]struct{}, jobCount)
	for i, result := range results {
		if errors[i] != nil {
			t.Fatalf("worker %d claim: %v", i, errors[i])
		}
		for _, job := range result {
			if _, exists := claimed[job.ID]; exists {
				t.Fatalf("job %s was claimed by both workers", job.ID)
			}
			claimed[job.ID] = struct{}{}
		}
	}
	if len(claimed) != jobCount {
		t.Fatalf("claimed %d jobs, want %d", len(claimed), jobCount)
	}
	for _, id := range ids {
		if _, exists := claimed[id]; !exists {
			t.Errorf("job %s was not claimed", id)
		}
	}
}

func TestSucceedRequiresLeaseOwner(t *testing.T) {
	if os.Getenv("RUN_JOBS_INTEGRATION") != "1" {
		t.Skip("set RUN_JOBS_INTEGRATION=1 to run database integration tests")
	}

	db, err := config.OpenDB()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	workerID := "succeed-test-worker"
	lockedUntil := time.Now().Add(time.Minute)
	job := models.Job{
		ID:          uuid.New(),
		Type:        "succeed_integration_test",
		OrderID:     uuid.New(),
		Status:      "running",
		Attempts:    1,
		LockedBy:    &workerID,
		LockedUntil: &lockedUntil,
	}
	if err := db.Table("orders.jobs").Create(&job).Error; err != nil {
		t.Fatalf("insert job: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Table("orders.jobs").Where("id = ?", job.ID).Delete(&models.Job{}).Error; err != nil {
			t.Errorf("delete test job: %v", err)
		}
	})

	store := NewStore(db)
	succeeded, err := store.Succeed(context.Background(), job.ID, "different-worker")
	if err != nil {
		t.Fatalf("succeed with wrong worker: %v", err)
	}
	if succeeded {
		t.Fatal("wrong worker succeeded the job")
	}

	succeeded, err = store.Succeed(context.Background(), job.ID, workerID)
	if err != nil {
		t.Fatalf("succeed with lease owner: %v", err)
	}
	if !succeeded {
		t.Fatal("lease owner did not succeed the job")
	}

	var stored models.Job
	if err := db.Table("orders.jobs").Where("id = ?", job.ID).First(&stored).Error; err != nil {
		t.Fatalf("load succeeded job: %v", err)
	}
	if stored.Status != "succeeded" {
		t.Errorf("status = %q, want succeeded", stored.Status)
	}
	if stored.LockedBy != nil || stored.LockedUntil != nil {
		t.Error("succeeded job retained its lease")
	}
}

func TestRescheduleUpdatesScheduleAndAttemptCount(t *testing.T) {
	if os.Getenv("RUN_JOBS_INTEGRATION") != "1" {
		t.Skip("set RUN_JOBS_INTEGRATION=1 to run database integration tests")
	}

	db, err := config.OpenDB()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	workerID := "reschedule-test-worker"
	lockedUntil := time.Now().Add(time.Minute)
	testJobs := []models.Job{
		{
			ID:          uuid.New(),
			Type:        "reschedule_integration_test",
			OrderID:     uuid.New(),
			Status:      "running",
			Attempts:    2,
			LockedBy:    &workerID,
			LockedUntil: &lockedUntil,
		},
		{
			ID:          uuid.New(),
			Type:        "reschedule_integration_test",
			OrderID:     uuid.New(),
			Status:      "running",
			Attempts:    2,
			LockedBy:    &workerID,
			LockedUntil: &lockedUntil,
		},
	}
	if err := db.Table("orders.jobs").Create(&testJobs).Error; err != nil {
		t.Fatalf("insert jobs: %v", err)
	}
	ids := []uuid.UUID{testJobs[0].ID, testJobs[1].ID}
	t.Cleanup(func() {
		if err := db.Table("orders.jobs").Where("id IN ?", ids).Delete(&models.Job{}).Error; err != nil {
			t.Errorf("delete test jobs: %v", err)
		}
	})

	store := NewStore(db)
	updated, err := store.Reschedule(
		context.Background(),
		testJobs[0].ID,
		"different-worker",
		time.Minute,
		false,
		"still pending",
	)
	if err != nil {
		t.Fatalf("reschedule with wrong worker: %v", err)
	}
	if updated {
		t.Fatal("wrong worker rescheduled the job")
	}

	for i, countAttempt := range []bool{false, true} {
		updated, err := store.Reschedule(
			context.Background(),
			testJobs[i].ID,
			workerID,
			time.Minute,
			countAttempt,
			"reschedule test error",
		)
		if err != nil {
			t.Fatalf("reschedule job %d: %v", i, err)
		}
		if !updated {
			t.Fatalf("lease owner did not reschedule job %d", i)
		}
	}

	var stored []models.Job
	if err := db.Table("orders.jobs").Where("id IN ?", ids).Order("id").Find(&stored).Error; err != nil {
		t.Fatalf("load rescheduled jobs: %v", err)
	}
	if len(stored) != len(testJobs) {
		t.Fatalf("loaded %d jobs, want %d", len(stored), len(testJobs))
	}

	attemptsByID := make(map[uuid.UUID]int, len(stored))
	for _, job := range stored {
		if job.Status != "queued" {
			t.Errorf("job %s status = %q, want queued", job.ID, job.Status)
		}
		if job.LockedBy != nil || job.LockedUntil != nil {
			t.Errorf("job %s retained its lease", job.ID)
		}
		if job.LastError == nil || *job.LastError != "reschedule test error" {
			t.Errorf("job %s last error was not stored", job.ID)
		}
		if !job.NextRunAt.After(time.Now().Add(30 * time.Second)) {
			t.Errorf("job %s was not delayed using database time", job.ID)
		}
		attemptsByID[job.ID] = job.Attempts
	}
	if got := attemptsByID[testJobs[0].ID]; got != 1 {
		t.Errorf("non-counted attempts = %d, want 1", got)
	}
	if got := attemptsByID[testJobs[1].ID]; got != 2 {
		t.Errorf("counted attempts = %d, want 2", got)
	}
}

func TestKillRequiresLeaseOwner(t *testing.T) {
	if os.Getenv("RUN_JOBS_INTEGRATION") != "1" {
		t.Skip("set RUN_JOBS_INTEGRATION=1 to run database integration tests")
	}

	db, err := config.OpenDB()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	workerID := "kill-test-worker"
	lockedUntil := time.Now().Add(time.Minute)
	job := models.Job{
		ID:          uuid.New(),
		Type:        "kill_integration_test",
		OrderID:     uuid.New(),
		Status:      "running",
		Attempts:    1,
		LockedBy:    &workerID,
		LockedUntil: &lockedUntil,
	}
	if err := db.Table("orders.jobs").Create(&job).Error; err != nil {
		t.Fatalf("insert job: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Table("orders.jobs").Where("id = ?", job.ID).Delete(&models.Job{}).Error; err != nil {
			t.Errorf("delete test job: %v", err)
		}
	})

	store := NewStore(db)
	killed, err := store.Kill(context.Background(), job.ID, "different-worker", "permanent failure")
	if err != nil {
		t.Fatalf("kill with wrong worker: %v", err)
	}
	if killed {
		t.Fatal("wrong worker killed the job")
	}

	killed, err = store.Kill(context.Background(), job.ID, workerID, "permanent failure")
	if err != nil {
		t.Fatalf("kill with lease owner: %v", err)
	}
	if !killed {
		t.Fatal("lease owner did not kill the job")
	}

	var stored models.Job
	if err := db.Table("orders.jobs").Where("id = ?", job.ID).First(&stored).Error; err != nil {
		t.Fatalf("load dead job: %v", err)
	}
	if stored.Status != "dead" {
		t.Errorf("status = %q, want dead", stored.Status)
	}
	if stored.LockedBy != nil || stored.LockedUntil != nil {
		t.Error("dead job retained its lease")
	}
	if stored.LastError == nil || *stored.LastError != "permanent failure" {
		t.Error("dead job did not store its last error")
	}
}

func TestListDeadReturnsOldestDeadJobsWithErrors(t *testing.T) {
	if os.Getenv("RUN_JOBS_INTEGRATION") != "1" {
		t.Skip("set RUN_JOBS_INTEGRATION=1 to run database integration tests")
	}

	db, err := config.OpenDB()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	if err := tx.Exec("DELETE FROM orders.jobs").Error; err != nil {
		t.Fatalf("isolate jobs in transaction: %v", err)
	}

	firstError := "first dead job"
	secondError := "second dead job"
	createdAt := time.Now().Add(-time.Hour)
	jobs := []models.Job{
		{
			ID:        uuid.New(),
			Type:      "list_dead_integration_test",
			OrderID:   uuid.New(),
			Status:    "dead",
			LastError: &firstError,
			CreatedAt: createdAt,
		},
		{
			ID:        uuid.New(),
			Type:      "list_dead_integration_test",
			OrderID:   uuid.New(),
			Status:    "dead",
			LastError: &secondError,
			CreatedAt: createdAt.Add(time.Second),
		},
		{
			ID:        uuid.New(),
			Type:      "list_dead_integration_test",
			OrderID:   uuid.New(),
			Status:    "queued",
			CreatedAt: createdAt.Add(-time.Second),
		},
	}
	if err := tx.Table("orders.jobs").Create(&jobs).Error; err != nil {
		t.Fatalf("insert jobs: %v", err)
	}

	store := NewStore(tx)
	dead, err := store.ListDead(context.Background(), 1)
	if err != nil {
		t.Fatalf("list dead jobs: %v", err)
	}
	if len(dead) != 1 {
		t.Fatalf("listed %d dead jobs, want 1", len(dead))
	}
	if dead[0].ID != jobs[0].ID {
		t.Fatalf("listed job %s, want oldest dead job %s", dead[0].ID, jobs[0].ID)
	}
	if dead[0].LastError == nil || *dead[0].LastError != firstError {
		t.Fatalf("last error = %v, want %q", dead[0].LastError, firstError)
	}
	if _, err := store.ListDead(context.Background(), 0); err == nil {
		t.Fatal("ListDead accepted a zero limit")
	}
}

func TestRequeueResetsDeadJobAndPreservesIdentity(t *testing.T) {
	if os.Getenv("RUN_JOBS_INTEGRATION") != "1" {
		t.Skip("set RUN_JOBS_INTEGRATION=1 to run database integration tests")
	}

	db, err := config.OpenDB()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	workerID := "stale-worker"
	lockedUntil := time.Now().Add(time.Minute)
	lastError := "retries exhausted: payment service unavailable"
	payload := datatypes.JSON(`{"source":"integration-test"}`)
	job := models.Job{
		ID:          uuid.New(),
		Type:        "requeue_integration_test",
		OrderID:     uuid.New(),
		Payload:     payload,
		Status:      "dead",
		Attempts:    5,
		MaxAttempts: 7,
		LockedBy:    &workerID,
		LockedUntil: &lockedUntil,
		LastError:   &lastError,
	}
	queuedJob := models.Job{
		ID:      uuid.New(),
		Type:    "requeue_integration_test",
		OrderID: uuid.New(),
		Status:  "queued",
	}
	if err := db.Table("orders.jobs").Create(&[]models.Job{job, queuedJob}).Error; err != nil {
		t.Fatalf("insert jobs: %v", err)
	}
	ids := []uuid.UUID{job.ID, queuedJob.ID}
	t.Cleanup(func() {
		if err := db.Table("orders.jobs").Where("id IN ?", ids).Delete(&models.Job{}).Error; err != nil {
			t.Errorf("delete test jobs: %v", err)
		}
	})

	var before time.Time
	if err := db.Raw("SELECT now()").Scan(&before).Error; err != nil {
		t.Fatalf("read database time before requeue: %v", err)
	}

	store := NewStore(db)
	requeued, err := store.Requeue(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("requeue dead job: %v", err)
	}
	if !requeued {
		t.Fatal("dead job was not requeued")
	}

	var after time.Time
	if err := db.Raw("SELECT now()").Scan(&after).Error; err != nil {
		t.Fatalf("read database time after requeue: %v", err)
	}

	var stored models.Job
	if err := db.Table("orders.jobs").Where("id = ?", job.ID).First(&stored).Error; err != nil {
		t.Fatalf("load requeued job: %v", err)
	}
	if stored.Status != "queued" || stored.Attempts != 0 {
		t.Errorf("requeued state = status %q attempts %d", stored.Status, stored.Attempts)
	}
	if stored.NextRunAt.Before(before) || stored.NextRunAt.After(after) {
		t.Errorf("next run time %s is outside database interval [%s, %s]", stored.NextRunAt, before, after)
	}
	if stored.LockedBy != nil || stored.LockedUntil != nil {
		t.Error("requeued job retained stale ownership")
	}
	if stored.ID != job.ID || stored.Type != job.Type || stored.OrderID != job.OrderID || stored.MaxAttempts != job.MaxAttempts {
		t.Error("requeue changed job identity")
	}
	var storedPayload, originalPayload bytes.Buffer
	if err := json.Compact(&storedPayload, stored.Payload); err != nil {
		t.Fatalf("compact stored payload: %v", err)
	}
	if err := json.Compact(&originalPayload, payload); err != nil {
		t.Fatalf("compact original payload: %v", err)
	}
	if !bytes.Equal(storedPayload.Bytes(), originalPayload.Bytes()) {
		t.Errorf("payload = %s, want %s", stored.Payload, payload)
	}
	if stored.LastError == nil || *stored.LastError != lastError {
		t.Error("requeue did not preserve the last error")
	}

	if requeued, err := store.Requeue(context.Background(), job.ID); err != nil || requeued {
		t.Fatalf("second requeue = %v, %v; want false, nil", requeued, err)
	}
	if requeued, err := store.Requeue(context.Background(), queuedJob.ID); err != nil || requeued {
		t.Fatalf("queued job requeue = %v, %v; want false, nil", requeued, err)
	}
	if requeued, err := store.Requeue(context.Background(), uuid.New()); err != nil || requeued {
		t.Fatalf("missing job requeue = %v, %v; want false, nil", requeued, err)
	}
}

func TestConcurrentRequeueOnlyUpdatesDeadJobOnce(t *testing.T) {
	if os.Getenv("RUN_JOBS_INTEGRATION") != "1" {
		t.Skip("set RUN_JOBS_INTEGRATION=1 to run database integration tests")
	}

	db, err := config.OpenDB()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	job := models.Job{
		ID:       uuid.New(),
		Type:     "concurrent_requeue_integration_test",
		OrderID:  uuid.New(),
		Status:   "dead",
		Attempts: 5,
	}
	if err := db.Table("orders.jobs").Create(&job).Error; err != nil {
		t.Fatalf("insert job: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Table("orders.jobs").Where("id = ?", job.ID).Delete(&models.Job{}).Error; err != nil {
			t.Errorf("delete test job: %v", err)
		}
	})

	store := NewStore(db)
	results := make([]bool, 2)
	errors := make([]error, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := range results {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			<-start
			results[i], errors[i] = store.Requeue(context.Background(), job.ID)
		}(i)
	}
	close(start)
	workers.Wait()

	successes := 0
	for i := range results {
		if errors[i] != nil {
			t.Fatalf("requeue %d: %v", i, errors[i])
		}
		if results[i] {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful requeues = %d, want 1", successes)
	}
}
