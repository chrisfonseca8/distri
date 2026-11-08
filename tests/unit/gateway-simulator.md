# Tests — `gateway-simulator`

4 test files · 34 top-level test functions · 34 pass · 34 unit, 0 integration (need Postgres)

"Kind" is *integration* when the test is skipped in a plain `go test` run and only runs against a database (`DATABASE_TEST_URL` plus the matching `RUN_*_INTEGRATION=1` flag). "What it checks" is the comment above the test function; where there is none it is derived from the test name and marked so.

[← all test suites](README.md)

### [`internal/sim/plan_test.go`](../../gateway-simulator/internal/sim/plan_test.go)

Package `gateway-simulator/internal/sim` · 7 test function(s) · 7 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPlanValidation` | unit | PASS | – | Plan Validation *(from the test name)* |
| `TestScriptedDeclineThenCaptureUsesExactTimes` | unit | PASS | – | Scripted Decline Then Capture Uses Exact Times *(from the test name)* |
| `TestOrderPlanCanPayAnExistingDuplicate` | unit | PASS | – | Order Plan Can Pay An Existing Duplicate *(from the test name)* |
| `TestDeliveryOrderIsIndependentOfTransitionOrder` | unit | PASS | – | Delivery Order Is Independent Of Transition Order *(from the test name)* |
| `TestOmittedEventIsListedButNeverSent` | unit | PASS | – | Omitted Event Is Listed But Never Sent *(from the test name)* |
| `TestScriptedDuplicatesAreByteIdentical` | unit | PASS | – | Scripted Duplicates Are Byte Identical *(from the test name)* |
| `TestDifferentEventsAboutOnePaymentCanArriveConcurrently` | unit | PASS | – | Different Events About One Payment Can Arrive Concurrently *(from the test name)* |

### [`internal/sim/refund_test.go`](../../gateway-simulator/internal/sim/refund_test.go)

Package `gateway-simulator/internal/sim` · 7 test function(s) · 7 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRefundOutcomesFollowTheScript` | unit | PASS | 3 | Refund Outcomes Follow The Script *(from the test name)* |
| `TestRefundIdempotencyKeyAndDuplicates` | unit | PASS | 3 | Refund Idempotency Key And Duplicates *(from the test name)* |
| `TestRefundResponseFaultsNeverUndoTheCommit` | unit | PASS | 5 | Refund Response Faults Never Undo The Commit *(from the test name)* |
| `TestDelayedVisibilityHidesTheRefundFromLookupsButNotFromTheAmount` | unit | PASS | – | Delayed Visibility Hides The Refund From Lookups But Not From The Amount *(from the test name)* |
| `TestRefundEventsComeFromTheStoredRefund` | unit | PASS | – | Refund Events Come From The Stored Refund *(from the test name)* |
| `TestRefundWebhookDeliveryCanBeDelayedOmittedAndReordered` | unit | PASS | – | Refund Webhook Delivery Can Be Delayed Omitted And Reordered *(from the test name)* |
| `TestRefundResponseFaultsOverHTTP` | unit | PASS | – | Refund Response Faults Over HTTP *(from the test name)* |

### [`internal/sim/store_test.go`](../../gateway-simulator/internal/sim/store_test.go)

Package `gateway-simulator/internal/sim` · 11 test function(s) · 11 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCreateOrderValidation` | unit | PASS | 7 | Create Order Validation *(from the test name)* |
| `TestDuplicateReceiptCompatibility` | unit | PASS | 2 | Duplicate Receipt Compatibility *(from the test name)* |
| `TestLifecycleKeepsEveryReadEndpointConsistent` | unit | PASS | – | Lifecycle Keeps Every Read Endpoint Consistent *(from the test name)* |
| `TestScenarios` | unit | PASS | 3 | Scenarios *(from the test name)* |
| `TestManualCaptureAccount` | unit | PASS | – | Manual Capture Account *(from the test name)* |
| `TestReceiptLookupLag` | unit | PASS | – | Receipt Lookup Lag *(from the test name)* |
| `TestRefundRules` | unit | PASS | – | Refund Rules *(from the test name)* |
| `TestRefundRequiresCapture` | unit | PASS | – | Refund Requires Capture *(from the test name)* |
| `TestConcurrentRefundsNeverExceedTheCapturedAmount` | unit | PASS | – | Concurrent Refunds Never Exceed The Captured Amount *(from the test name)* |
| `TestDecisionsAreReproducibleAndIndependentOfCreationOrder` | unit | PASS | – | Decisions Are Reproducible And Independent Of Creation Order *(from the test name)* |
| `TestSchedulerRunsTasksInTimeOrder` | unit | PASS | – | Scheduler Runs Tasks In Time Order *(from the test name)* |

### [`internal/sim/webhook_test.go`](../../gateway-simulator/internal/sim/webhook_test.go)

Package `gateway-simulator/internal/sim` · 9 test function(s) · 9 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestDeliverySignsTheExactBytesAndSendsTheEventID` | unit | PASS | – | Delivery Signs The Exact Bytes And Sends The Event ID *(from the test name)* |
| `TestRetriesKeepTheEventIdentityAndPayload` | unit | PASS | – | Retries Keep The Event Identity And Payload *(from the test name)* |
| `TestAcknowledgementTimeoutIsRetried` | unit | PASS | – | Acknowledgement Timeout Is Retried *(from the test name)* |
| `TestUnreachableReceiverIsRetriedUntilTheMaxAge` | unit | PASS | – | Unreachable Receiver Is Retried Until The Max Age *(from the test name)* |
| `TestDeliveryConcurrencyIsBounded` | unit | PASS | – | Delivery Concurrency Is Bounded *(from the test name)* |
| `TestRedeliverySendsTheOriginalBytesAndEventID` | unit | PASS | – | Redelivery Sends The Original Bytes And Event ID *(from the test name)* |
| `TestEventsComeFromCommittedTransitionsAndKeepTheirSnapshot` | unit | PASS | – | Events Come From Committed Transitions And Keep Their Snapshot *(from the test name)* |
| `TestDeclineEmitsAFailedEventWithErrorFields` | unit | PASS | – | Decline Emits A Failed Event With Error Fields *(from the test name)* |
| `TestDuplicateOrderPolicyIsSeparateFromTheObservedReceiptBehaviour` | unit | PASS | 3 | Duplicate Order Policy Is Separate From The Observed Receipt Behaviour *(from the test name)* |
