package jobs

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"github.com/chrisfonseca08/distri/order-service/pkg/config"
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
