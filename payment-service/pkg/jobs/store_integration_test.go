package jobs

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/config"
	"github.com/chrisfonseca08/distri/payment-service/pkg/jobtypes"
)

func TestClaimConcurrentWorkers(t *testing.T) {
	if os.Getenv("RUN_PAYMENT_JOBS_INTEGRATION") != "1" {
		t.Skip("set RUN_PAYMENT_JOBS_INTEGRATION=1 to run database integration tests")
	}
	db, err := config.OpenDB()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := config.RequireDedicatedTestDB(db); err != nil {
		t.Skip(err)
	}

	const processCount = 10
	testDueAt := time.Unix(1, 0).UTC()
	testJobs := make([]models.Job, 0, processCount)
	ids := make([]uuid.UUID, 0, processCount)
	for range processCount {
		id := uuid.New()
		ids = append(ids, id)
		testJobs = append(testJobs, models.Job{
			ID: id, Type: jobtypes.ProcessPayment, PaymentID: uuid.New(), NextRunAt: testDueAt,
		})
	}
	if err := db.Table("payments.jobs").Create(&testJobs).Error; err != nil {
		t.Fatalf("insert jobs: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Table("payments.jobs").Where("id IN ?", ids).Delete(&models.Job{}).Error; err != nil {
			t.Errorf("delete jobs: %v", err)
		}
	})

	store := NewStore(db)
	results := make([][]models.Job, 2)
	errors := make([]error, 2)
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	for index := range results {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			<-start
			results[index], errors[index] = store.Claim(
				context.Background(),
				"payment-claim-worker-"+string(rune('A'+index)),
				processCount,
				time.Minute,
			)
		}(index)
	}
	close(start)
	waitGroup.Wait()

	claimed := make(map[uuid.UUID]struct{}, processCount)
	for index, result := range results {
		if errors[index] != nil {
			t.Fatalf("worker %d claim: %v", index, errors[index])
		}
		for _, job := range result {
			if job.Type != jobtypes.ProcessPayment {
				t.Fatalf("worker claimed unsupported job type %q", job.Type)
			}
			if _, exists := claimed[job.ID]; exists {
				t.Fatalf("job %s was claimed twice", job.ID)
			}
			claimed[job.ID] = struct{}{}
		}
	}
	if len(claimed) != processCount {
		t.Fatalf("claimed %d process jobs, want %d", len(claimed), processCount)
	}
}
