# Tests — `inventory-service`

7 test files · 12 top-level test functions · 12 pass · 5 unit, 7 integration (need Postgres)

"Kind" is *integration* when the test is skipped in a plain `go test` run and only runs against a database (`DATABASE_TEST_URL` plus the matching `RUN_*_INTEGRATION=1` flag). "What it checks" is the comment above the test function; where there is none it is derived from the test name and marked so.

[← all test suites](README.md)

### [`pkg/observability/middleware_test.go`](../../inventory-service/pkg/observability/middleware_test.go)

Package `inventory-service/pkg/observability` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCorrelationSurvivesHTTPAndAMQP` | unit | PASS | – | Correlation Survives HTTP And AMQP *(from the test name)* |
| `TestLoggerAddsCorrelationAndRedactsSecrets` | unit | PASS | – | Logger Adds Correlation And Redacts Secrets *(from the test name)* |
| `TestCorrelationJSONRoundTrip` | unit | PASS | – | Correlation JSON Round Trip *(from the test name)* |

### [`pkg/retention/retention_integration_test.go`](../../inventory-service/pkg/retention/retention_integration_test.go)

Package `inventory-service/pkg/retention` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPurgeRemovesOnlyExpiredMessageBookkeeping` | integration (needs Postgres) | PASS | – | Purge Removes Only Expired Message Bookkeeping *(from the test name)* |

### [`pkg/services/conflict_retry_test.go`](../../inventory-service/pkg/services/conflict_retry_test.go)

Package `inventory-service/pkg/services` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRetryOnConflictIsBounded` | unit | PASS | – | Retry On Conflict Is Bounded *(from the test name)* |
| `TestRetryOnConflictRetriesOnlyConflictsAndStopsOnSuccess` | unit | PASS | – | Retry On Conflict Retries Only Conflicts And Stops On Success *(from the test name)* |

### [`pkg/services/deadlock_integration_test.go`](../../inventory-service/pkg/services/deadlock_integration_test.go)

Package `inventory-service/pkg/services` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestReversedCartsDeadlockWithoutLockOrder` | integration (needs Postgres) | PASS | – | REPRODUCTION: no sorted order, no retry (a single attempt) -> PostgreSQL aborts one with 40P01. |
| `TestReversedCartsDoNotDeadlockWithSortedLockOrder` | integration (needs Postgres) | PASS | – | FIX 1: the sorted lock order removes the cycle, so there is nothing to retry. |
| `TestDeadlockVictimIsRetriedAndSucceeds` | integration (needs Postgres) | PASS | – | FIX 2 (defence in depth): if a cycle ever forms again, the victim is re-run a bounded number of times and both checkouts succeed. |

### [`pkg/services/oversell_integration_test.go`](../../inventory-service/pkg/services/oversell_integration_test.go)

Package `inventory-service/pkg/services` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestConcurrentBuyersCannotOversellLastTenUnits` | integration (needs Postgres) | PASS | – | Two hundred buyers race for the final ten units. The conditional inventory update is the serialization point: exactly ten reservations may win. |

### [`pkg/services/reservation_sweeper_integration_test.go`](../../inventory-service/pkg/services/reservation_sweeper_integration_test.go)

Package `inventory-service/pkg/services` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestListOldReservedAndConcurrentReleaseIsIdempotent` | integration (needs Postgres) | PASS | – | List Old Reserved And Concurrent Release Is Idempotent *(from the test name)* |

### [`pkg/services/restock_integration_test.go`](../../inventory-service/pkg/services/restock_integration_test.go)

Package `inventory-service/pkg/services` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestReleasingAConfirmedReservationRestocksTheUnitsOnce` | integration (needs Postgres) | PASS | – | A checkout that was paid, then refunded and cancelled, gives its units back. |
