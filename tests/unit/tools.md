# Tests — `tools`

9 test files · 24 top-level test functions · 24 pass · 24 unit, 0 integration (need Postgres)

"Kind" is *integration* when the test is skipped in a plain `go test` run and only runs against a database (`DATABASE_TEST_URL` plus the matching `RUN_*_INTEGRATION=1` flag). "What it checks" is the comment above the test function; where there is none it is derived from the test name and marked so.

[← all test suites](README.md)

### [`correctness/docker_control_test.go`](../../tools/correctness/docker_control_test.go)

Package `tools/correctness` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestDockerContainerActionThroughUnixSocket` | unit | PASS | – | Docker Container Action Through Unix Socket *(from the test name)* |
| `TestCorrectnessResetRequested` | unit | PASS | – | Correctness Reset Requested *(from the test name)* |

### [`correctness/http_test.go`](../../tools/correctness/http_test.go)

Package `tools/correctness` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestWaitPaymentContinuesThroughAwaitingPayment` | unit | PASS | – | Wait Payment Continues Through Awaiting Payment *(from the test name)* |

### [`correctness/report_test.go`](../../tools/correctness/report_test.go)

Package `tools/correctness` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestConsolidateReportsAcceptsCollections` | unit | PASS | – | Consolidate Reports Accepts Collections *(from the test name)* |

### [`load-test/runner/container_metrics_test.go`](../../tools/load-test/runner/container_metrics_test.go)

Package `tools/load-test/runner` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRecordComputesCPUFromConsecutiveSamples` | unit | PASS | – | A container with a large lifetime CPU total but no CPU use between two samples must read as idle: the old one-shot formula reported lifetime averages instead. |
| `TestFirstSampleOnlyEstablishesBaseline` | unit | PASS | – | First Sample Only Establishes Baseline *(from the test name)* |
| `TestMissingPostgresContainerIsOptionalAndExplained` | unit | PASS | – | Missing Postgres Container Is Optional And Explained *(from the test name)* |

### [`load-test/runner/http_correlation_test.go`](../../tools/load-test/runner/http_correlation_test.go)

Package `tools/load-test/runner` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestLoadClientAddsTestRunHeader` | unit | PASS | – | Load Client Adds Test Run Header *(from the test name)* |

### [`load-test/runner/http_sse_test.go`](../../tools/load-test/runner/http_sse_test.go)

Package `tools/load-test/runner` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestStreamTerminalReadsCompletedEvent` | unit | PASS | – | Stream Terminal Reads Completed Event *(from the test name)* |

### [`load-test/runner/percentiles_test.go`](../../tools/load-test/runner/percentiles_test.go)

Package `tools/load-test/runner` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestSummarizeLatencies` | unit | PASS | – | Summarize Latencies *(from the test name)* |

### [`load-test/runner/report_test.go`](../../tools/load-test/runner/report_test.go)

Package `tools/load-test/runner` · 12 test function(s) · 12 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCalculateBaselineVarianceIgnoresSmoke` | unit | PASS | – | Calculate Baseline Variance Ignores Smoke *(from the test name)* |
| `TestRunPassedRequiresExactCompletionAndCleanInvariants` | unit | PASS | – | Run Passed Requires Exact Completion And Clean Invariants *(from the test name)* |
| `TestRunPassedAcceptsExactC5bSellout` | unit | PASS | – | Run Passed Accepts Exact C5b Sellout *(from the test name)* |
| `TestSweepDetection` | unit | PASS | – | Sweep Detection *(from the test name)* |
| `TestExperimentMatrix` | unit | PASS | – | Experiment Matrix *(from the test name)* |
| `TestScenarioPointsSelectAndFlattenCases` | unit | PASS | – | Scenario Points Select And Flatten Cases *(from the test name)* |
| `TestRepeatHistoryKeepsEarlierRunsAndSpread` | unit | PASS | – | Repeat History Keeps Earlier Runs And Spread *(from the test name)* |
| `TestCheckDatabaseTargetRefusesUnlistedRemoteHosts` | unit | PASS | – | Check Database Target Refuses Unlisted Remote Hosts *(from the test name)* |
| `TestCustomSweepSteps` | unit | PASS | – | Custom Sweep Steps *(from the test name)* |
| `TestLoadConfigAcceptsC3SweepSteps` | unit | PASS | – | C3 takes its client count from each sweep step, so the case itself has no client count; this must not be rejected as "load parameters must be positive". |
| `TestClientOverrideAndConnectionCap` | unit | PASS | – | Client Override And Connection Cap *(from the test name)* |
| `TestParseCPUStatUsage` | unit | PASS | – | Parse CPU Stat Usage *(from the test name)* |

### [`load-test/runner/workload_test.go`](../../tools/load-test/runner/workload_test.go)

Package `tools/load-test/runner` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestSkewedSelectorIsExactEightyTwenty` | unit | PASS | – | Skewed Selector Is Exact Eighty Twenty *(from the test name)* |
| `TestMultiItemSelectorUsesDistinctHotProducts` | unit | PASS | – | Multi Item Selector Uses Distinct Hot Products *(from the test name)* |
