package poolscaler

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestLatencyGuardLearnsFromHealthyProbesOnlyAndPausesOnASlowDatabase(t *testing.T) {
	g := NewDBGuard(nil, testCfg(), nil)
	for i := 0; i < 3; i++ { // warm-up establishes the baseline: the fastest of three
		if pause, _ := g.latencyVerdict([]time.Duration{3, 2, 4}[i] * time.Millisecond); pause {
			t.Fatal("paused during warm-up")
		}
	}
	if g.baseline != 2*time.Millisecond {
		t.Fatalf("baseline %s, want 2ms", g.baseline)
	}
	if pause, _ := g.latencyVerdict(3 * time.Millisecond); pause {
		t.Fatal("paused at 1.5x the baseline (factor is 3)")
	}
	before := g.baseline
	if pause, reason := g.latencyVerdict(30 * time.Millisecond); !pause || !strings.Contains(reason, "baseline") {
		t.Fatalf("30ms against a 2ms baseline: pause=%v reason=%q, want a pause", pause, reason)
	}
	if g.baseline != before {
		t.Fatal("a slow probe moved the baseline: the guard would learn that slow is normal")
	}
	g.baseline = 40 * time.Millisecond // even 3x of a high baseline cannot exceed the absolute maximum
	if pause, _ := g.latencyVerdict(150 * time.Millisecond); !pause {
		t.Fatal("150ms exceeds the 100ms absolute limit but did not pause")
	}
}

func TestConnectionErrorCounterSumsOnlyConnectionKindsFromDBErrorsTotal(t *testing.T) {
	registry := prometheus.NewRegistry()
	errors := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "db_errors_total", Help: "x"}, []string{"operation", "kind"})
	registry.MustRegister(errors)
	errors.WithLabelValues("query", "connection").Add(2)
	errors.WithLabelValues("transaction", "timeout").Add(3)
	errors.WithLabelValues("query", "pg_53300").Add(1) // too_many_connections
	errors.WithLabelValues("query", "unique_violation").Add(50)
	errors.WithLabelValues("query", "deadlock").Add(40)
	total, err := ConnectionErrorCounter(registry)()
	if err != nil || total != 6 {
		t.Fatalf("total=%v err=%v, want 6 (connection 2 + timeout 3 + pg_53300 1)", total, err)
	}
}

func TestGuardReportsUnknownWhenItsProbeCannotRun(t *testing.T) {
	probe, err := sql.Open("pgx", "postgres://nobody:nothing@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	result := NewDBGuard(probe, testCfg(), nil).Check(context.Background(), 5)
	if !result.Unknown || result.Pause {
		t.Fatalf("unreachable database: %+v, want Unknown (increases held, nothing else claimed)", result)
	}
}

func openTestProbe(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_TEST_URL")
	if dsn == "" {
		t.Skip("DATABASE_TEST_URL is not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Skipf("test database unavailable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	var name string
	if err := db.QueryRow("SELECT current_database()").Scan(&name); err != nil {
		t.Skipf("test database unavailable: %v", err)
	}
	if !strings.HasSuffix(name, "_test") {
		t.Skip("not a dedicated *_test database")
	}
	return db
}

func TestGuardAgainstARealDatabaseSeesLatencyLocksAndTheConnectionBudget(t *testing.T) {
	admin := openTestProbe(t)
	probe := openTestProbe(t)
	probe.SetMaxOpenConns(1)
	cfg := testCfg()
	cfg.GuardLockWaiters = 1
	cfg.Budget = 100000
	guard := NewDBGuard(probe, cfg, nil)
	for i := 0; i < 4; i++ { // healthy: warm-up and a normal probe
		if r := guard.Check(context.Background(), 5); r.Pause || r.Unknown {
			t.Fatalf("healthy database reported %+v", r)
		}
	}

	// Budget: this database already has connections, so a budget of 1 cannot fit a step of 5.
	tight := cfg
	tight.Budget = 1
	if r := NewDBGuard(probe, tight, nil).Check(context.Background(), 5); !r.Pause || !strings.Contains(r.Reason, "connection budget") {
		t.Fatalf("tiny budget: %+v, want a pause naming the connection budget", r)
	}

	// Lock contention: one session holds a row, another waits for it.
	if _, err := admin.Exec("CREATE TABLE IF NOT EXISTS poolscaler_lock_test (id int primary key, v int)"); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP TABLE IF EXISTS poolscaler_lock_test")
	admin.Exec("DELETE FROM poolscaler_lock_test")
	admin.Exec("INSERT INTO poolscaler_lock_test VALUES (1, 0)")
	holder, err := admin.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback()
	if _, err := holder.Exec("UPDATE poolscaler_lock_test SET v = 1 WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	var waiter sync.WaitGroup
	waiter.Add(1)
	go func() {
		defer waiter.Done()
		other := openTestProbe(t)
		other.SetMaxOpenConns(1)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = other.ExecContext(ctx, "UPDATE poolscaler_lock_test SET v = 2 WHERE id = 1")
	}()
	deadline := time.Now().Add(3 * time.Second)
	var result GuardResult
	for time.Now().Before(deadline) {
		if result = guard.Check(context.Background(), 5); result.Pause {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !result.Pause || !strings.Contains(result.Reason, "lock") {
		t.Fatalf("a waiting lock request: %+v, want a pause naming lock requests", result)
	}
	holder.Rollback()
	waiter.Wait()
}

// A real *sql.DB under real contention: 30 workers share 5 connections. The scaler
// reads the real DB.Stats deltas and must grow the pool in steps of 5 and never past
// the ceiling, while MaxIdleConns follows the cap.
func TestScalerGrowsARealPoolThatIsBlockingAndStopsAtTheCeiling(t *testing.T) {
	db := openTestProbe(t)
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)
	cfg := testCfg()
	cfg.UpWait, cfg.UpMinWaits, cfg.UpSamples, cfg.UpCooldown = time.Millisecond, 5, 1, 0
	cfg.Budget, cfg.BudgetReserved, cfg.Replicas, cfg.Max = 100000, 0, 1, 20
	scaler, err := New(db, &fakeGuard{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	clock := base
	scaler.now = func() time.Time { return clock }

	ctx, cancel := context.WithCancel(context.Background())
	var load sync.WaitGroup
	for i := 0; i < 40; i++ {
		load.Add(1)
		go func() {
			defer load.Done()
			for ctx.Err() == nil {
				_, _ = db.ExecContext(ctx, "SELECT pg_sleep(0.05)")
			}
		}()
	}
	var sizes []int
	last := 5
	for i := 0; i < 30; i++ {
		time.Sleep(200 * time.Millisecond)
		clock = clock.Add(5 * time.Second)
		scaler.tick(context.Background())
		if now := db.Stats().MaxOpenConnections; now != last {
			sizes = append(sizes, now)
			last = now
		}
	}
	cancel()
	load.Wait()
	if len(sizes) == 0 || sizes[0] != 10 {
		t.Fatalf("pool sizes seen %v, want growth starting 5 -> 10", sizes)
	}
	for i, size := range sizes {
		if size%5 != 0 || size > 20 || (i > 0 && size-sizes[i-1] != 5) {
			t.Fatalf("pool sizes %v: want steps of 5 and never above the ceiling of 20", sizes)
		}
	}
	if last != 20 {
		t.Fatalf("pool ended at %d, want it grown to the ceiling of 20 under sustained contention", last)
	}
}
