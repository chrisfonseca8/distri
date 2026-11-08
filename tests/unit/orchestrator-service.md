# Tests — `orchestrator-service`

25 test files · 85 top-level test functions · 85 pass · 46 unit, 39 integration (need Postgres)

"Kind" is *integration* when the test is skipped in a plain `go test` run and only runs against a database (`DATABASE_TEST_URL` plus the matching `RUN_*_INTEGRATION=1` flag). "What it checks" is the comment above the test function; where there is none it is derived from the test name and marked so.

[← all test suites](README.md)

### [`pkg/admin/stuck_integration_test.go`](../../orchestrator-service/pkg/admin/stuck_integration_test.go)

Package `orchestrator-service/pkg/admin` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestReviveSendsAStuckSagasStepAgainAndOnlyThat` | integration (needs Postgres) | PASS | – | Revive Sends A Stuck Sagas Step Again And Only That *(from the test name)* |

### [`pkg/clients/inventory_client_test.go`](../../orchestrator-service/pkg/clients/inventory_client_test.go)

Package `orchestrator-service/pkg/clients` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestNewInventoryClientFromEnvRequiresInventoryURL` | unit | PASS | – | New Inventory Client From Env Requires Inventory URL *(from the test name)* |

### [`pkg/config/outbox_config_test.go`](../../orchestrator-service/pkg/config/outbox_config_test.go)

Package `orchestrator-service/pkg/config` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestLoadOutboxConcurrency` | unit | PASS | – | Load Outbox Concurrency *(from the test name)* |
| `TestLoadOutboxPollInterval` | unit | PASS | – | Load Outbox Poll Interval *(from the test name)* |

### [`pkg/config/saga_config_test.go`](../../orchestrator-service/pkg/config/saga_config_test.go)

Package `orchestrator-service/pkg/config` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestLoadSagaConfigDefaults` | unit | PASS | – | Load Saga Config Defaults *(from the test name)* |
| `TestSagaConfigRejectsInvalidDurations` | unit | PASS | – | Saga Config Rejects Invalid Durations *(from the test name)* |

### [`pkg/controllers/checkout_controller_test.go`](../../orchestrator-service/pkg/controllers/checkout_controller_test.go)

Package `orchestrator-service/pkg/controllers` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCreateCheckoutRejectsOversizedBody` | unit | PASS | – | Create Checkout Rejects Oversized Body *(from the test name)* |
| `TestGetCheckoutRejectsInvalidOrderID` | unit | PASS | – | Get Checkout Rejects Invalid Order ID *(from the test name)* |

### [`pkg/controllers/checkout_sse_edge_test.go`](../../orchestrator-service/pkg/controllers/checkout_sse_edge_test.go)

Package `orchestrator-service/pkg/controllers` · 8 test function(s) · 8 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestStreamForAnAlreadyTerminalSagaSendsOneEventAndClosesAtOnce` | unit | PASS | – | Stream For An Already Terminal Saga Sends One Event And Closes At Once *(from the test name)* |
| `TestStreamRejectsBadRequestsWithoutLeavingASubscriber` | unit | PASS | – | Stream Rejects Bad Requests Without Leaving A Subscriber *(from the test name)* |
| `TestStreamAtSubscriberCapacityAnswers503AndFreesTheSlotWhenAnotherEnds` | unit | PASS | – | Stream At Subscriber Capacity Answers503 And Frees The Slot When Another Ends *(from the test name)* |
| `TestClientDisconnectReleasesTheSubscriptionAndDoesNotLeakGoroutines` | unit | PASS | – | Client Disconnect Releases The Subscription And Does Not Leak Goroutines *(from the test name)* |
| `TestDuplicateAndStaleNotificationsNeverEmitRepeatedOrOlderVersions` | unit | PASS | – | Duplicate And Stale Notifications Never Emit Repeated Or Older Versions *(from the test name)* |
| `TestMidStreamSnapshotFailureEndsTheStreamAndReleasesTheSubscription` | unit | PASS | – | Mid Stream Snapshot Failure Ends The Stream And Releases The Subscription *(from the test name)* |
| `TestManyConcurrentStreamsAllReceiveTheTerminalEventAndLeaveNothingBehind` | unit | PASS | – | Many Concurrent Streams All Receive The Terminal Event And Leave Nothing Behind *(from the test name)* |
| `TestHeartbeatCommentsAreNotEventsAndKeepAnIdleStreamAlive` | unit | PASS | – | A raw reader makes sure the heartbeat comment is not mistaken for an event. |

### [`pkg/controllers/checkout_sse_test.go`](../../orchestrator-service/pkg/controllers/checkout_sse_test.go)

Package `orchestrator-service/pkg/controllers` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestStreamCheckoutSendsSnapshotThenCommittedChange` | unit | PASS | – | Stream Checkout Sends Snapshot Then Committed Change *(from the test name)* |

### [`pkg/messaging/consumer_test.go`](../../orchestrator-service/pkg/messaging/consumer_test.go)

Package `orchestrator-service/pkg/messaging` · 4 test function(s) · 4 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestConsumerAcknowledgesSuccessfulDelivery` | unit | PASS | – | Consumer Acknowledges Successful Delivery *(from the test name)* |
| `TestConsumerDeadLettersPermanentFailure` | unit | PASS | – | Consumer Dead Letters Permanent Failure *(from the test name)* |
| `TestConsumerRequeuesTransientFailureWhenRetryRepublishFails` | unit | PASS | – | Consumer Requeues Transient Failure When Retry Republish Fails *(from the test name)* |
| `TestNewConsumerValidatesRequiredValues` | unit | PASS | – | New Consumer Validates Required Values *(from the test name)* |

### [`pkg/messaging/pool_test.go`](../../orchestrator-service/pkg/messaging/pool_test.go)

Package `orchestrator-service/pkg/messaging` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestDesiredWorkersCanReachThirtyAndCapsThere` | unit | PASS | – | Desired Workers Can Reach Thirty And Caps There *(from the test name)* |

### [`pkg/observability/middleware_test.go`](../../orchestrator-service/pkg/observability/middleware_test.go)

Package `orchestrator-service/pkg/observability` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCorrelationSurvivesHTTPAndAMQP` | unit | PASS | – | Correlation Survives HTTP And AMQP *(from the test name)* |
| `TestLoggerAddsCorrelationAndRedactsSecrets` | unit | PASS | – | Logger Adds Correlation And Redacts Secrets *(from the test name)* |
| `TestCorrelationJSONRoundTrip` | unit | PASS | – | Correlation JSON Round Trip *(from the test name)* |

### [`pkg/outbox/relay_edge_integration_test.go`](../../orchestrator-service/pkg/outbox/relay_edge_integration_test.go)

Package `orchestrator-service/pkg/outbox` · 5 test function(s) · 5 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestTwoRelaysPublishEveryRowExactlyOnceWhenNothingCrashes` | integration (needs Postgres) | PASS | – | Two Relays Publish Every Row Exactly Once When Nothing Crashes *(from the test name)* |
| `TestRelayKilledAfterBrokerConfirmButBeforeTheMarkLosesNothingAndRepeatsWithTheSameMessageID` | integration (needs Postgres) | PASS | – | Relay Killed After Broker Confirm But Before The Mark Loses Nothing And Repeats With The Same Message ID *(from the test name)* |
| `TestAStaleRelayCannotMarkOrRescheduleARowAnotherRelayNowHolds` | integration (needs Postgres) | PASS | – | A Stale Relay Cannot Mark Or Reschedule A Row Another Relay Now Holds *(from the test name)* |
| `TestClaimedRowsAreNotClaimedAgainBeforeTheirLeaseExpires` | integration (needs Postgres) | PASS | – | Claimed Rows Are Not Claimed Again Before Their Lease Expires *(from the test name)* |
| `TestALeasedRowIsInvisibleToLaterClaimsUntilTheLeaseExpires` | integration (needs Postgres) | PASS | – | A Leased Row Is Invisible To Later Claims Until The Lease Expires *(from the test name)* |

### [`pkg/outbox/relay_test.go`](../../orchestrator-service/pkg/outbox/relay_test.go)

Package `orchestrator-service/pkg/outbox` · 5 test function(s) · 5 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRelayMarksConfirmedMessagePublished` | unit | PASS | – | Relay Marks Confirmed Message Published *(from the test name)* |
| `TestRelayReschedulesFailedPublish` | unit | PASS | – | Relay Reschedules Failed Publish *(from the test name)* |
| `TestRetryDelayCapsExponentialBackoff` | unit | PASS | – | Retry Delay Caps Exponential Backoff *(from the test name)* |
| `TestRelayReleasesUnpublishedBatchOnShutdown` | unit | PASS | – | Relay Releases Unpublished Batch On Shutdown *(from the test name)* |
| `TestRelayConfigRequiresLeaseForWholeBatch` | unit | PASS | – | Relay Config Requires Lease For Whole Batch *(from the test name)* |

### [`pkg/outbox/store_integration_test.go`](../../orchestrator-service/pkg/outbox/store_integration_test.go)

Package `orchestrator-service/pkg/outbox` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestEnqueueUsesCallerTransaction` | integration (needs Postgres) | PASS | – | Enqueue Uses Caller Transaction *(from the test name)* |
| `TestUnpublishableRowStopsAtLimitAndCanBeRequeued` | integration (needs Postgres) | PASS | – | Unpublishable Row Stops At Limit And Can Be Requeued *(from the test name)* |

### [`pkg/outbox/store_test.go`](../../orchestrator-service/pkg/outbox/store_test.go)

Package `orchestrator-service/pkg/outbox` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestEnqueueValidatesMessage` | unit | PASS | 3 | Enqueue Validates Message *(from the test name)* |

### [`pkg/retention/retention_integration_test.go`](../../orchestrator-service/pkg/retention/retention_integration_test.go)

Package `orchestrator-service/pkg/retention` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPurgeRemovesOnlyExpiredBookkeepingInBatches` | integration (needs Postgres) | PASS | – | Purge Removes Only Expired Bookkeeping In Batches *(from the test name)* |

### [`pkg/saga/state_test.go`](../../orchestrator-service/pkg/saga/state_test.go)

Package `orchestrator-service/pkg/saga` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestNext` | unit | PASS | 27 | Next *(from the test name)* |
| `TestNextRejectsUndefinedTransitions` | unit | PASS | 3 | Next Rejects Undefined Transitions *(from the test name)* |
| `TestTransitionTableIsCompleteAndEveryDecisionCanReachATerminalState` | unit | PASS | – | Every documented step/outcome pair must be defined, and every non-terminal decision must still have a path to a terminal saga status. This makes adding a step without completing its transition table fail loudly. |

### [`pkg/services/checkout/checkout_attempt_integration_test.go`](../../orchestrator-service/pkg/services/checkout/checkout_attempt_integration_test.go)

Package `orchestrator-service/pkg/services/checkout` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestClaimCheckoutAttemptIsIdempotentAndRejectsHashConflict` | integration (needs Postgres) | PASS | – | Claim Checkout Attempt Is Idempotent And Rejects Hash Conflict *(from the test name)* |

### [`pkg/services/checkout/checkout_query_integration_test.go`](../../orchestrator-service/pkg/services/checkout/checkout_query_integration_test.go)

Package `orchestrator-service/pkg/services/checkout` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestGetCheckoutReturnsSagaAndOrderedSteps` | integration (needs Postgres) | PASS | – | Get Checkout Returns Saga And Ordered Steps *(from the test name)* |
| `TestGetCheckoutReturnsNotFound` | integration (needs Postgres) | PASS | – | Get Checkout Returns Not Found *(from the test name)* |

### [`pkg/services/checkout/checkout_request_test.go`](../../orchestrator-service/pkg/services/checkout/checkout_request_test.go)

Package `orchestrator-service/pkg/services/checkout` · 4 test function(s) · 4 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPrepareCheckoutRequestSortsBeforeHashing` | unit | PASS | – | Prepare Checkout Request Sorts Before Hashing *(from the test name)* |
| `TestPrepareCheckoutRequestRejectsDuplicateProducts` | unit | PASS | – | Prepare Checkout Request Rejects Duplicate Products *(from the test name)* |
| `TestPrepareRejectsBadIdempotencyKeysAndLimitsWithCleanMessages` | unit | PASS | 5 | Prepare Rejects Bad Idempotency Keys And Limits With Clean Messages *(from the test name)* |
| `TestInventoryFailuresAreShownWithoutInventorysOwnText` | unit | PASS | – | Inventory Failures Are Shown Without Inventorys Own Text *(from the test name)* |

### [`pkg/services/checkout/checkout_service_integration_test.go`](../../orchestrator-service/pkg/services/checkout/checkout_service_integration_test.go)

Package `orchestrator-service/pkg/services/checkout` · 4 test function(s) · 4 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCreateCheckoutCommitsSagaStepsOutboxAndAttempt` | integration (needs Postgres) | PASS | – | Create Checkout Commits Saga Steps Outbox And Attempt *(from the test name)* |
| `TestCreateCheckoutStoresDefinitiveInventoryFailure` | integration (needs Postgres) | PASS | – | Create Checkout Stores Definitive Inventory Failure *(from the test name)* |
| `TestCreateCheckoutLeavesAmbiguousInventoryFailureInProgress` | integration (needs Postgres) | PASS | – | Create Checkout Leaves Ambiguous Inventory Failure In Progress *(from the test name)* |
| `TestCreateCheckoutConcurrentDuplicateReturnsWinningResponse` | integration (needs Postgres) | PASS | – | Create Checkout Concurrent Duplicate Returns Winning Response *(from the test name)* |

### [`pkg/services/reply/order_reply_integration_test.go`](../../orchestrator-service/pkg/services/reply/order_reply_integration_test.go)

Package `orchestrator-service/pkg/services/reply` · 6 test function(s) · 6 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestHandlerAdvancesSagaAndIsIdempotent` | integration (needs Postgres) | PASS | – | Handler Advances Saga And Is Idempotent *(from the test name)* |
| `TestHandlerIgnoresReplyForInactiveStep` | integration (needs Postgres) | PASS | – | Handler Ignores Reply For Inactive Step *(from the test name)* |
| `TestHandlerAcceptsLatePaymentSuccess` | unit | PASS | 3 | Handler Accepts Late Payment Success *(from the test name)* |
| `TestHandlerMarksConflictingPaymentSuccessStuck` | unit | PASS | 2 | Handler Marks Conflicting Payment Success Stuck *(from the test name)* |
| `TestHandlerTreatsMissingSagaAsPermanentAndRollsBackMessage` | integration (needs Postgres) | PASS | – | Handler Treats Missing Saga As Permanent And Rolls Back Message *(from the test name)* |
| `TestFailStepMovesACreateOrderSagaIntoCompensationOnce` | integration (needs Postgres) | PASS | – | FailStep is what lets the sweeper compensate a CREATE_ORDER that can no longer succeed: the saga must cancel the order first (it may exist) and only then release the stock. |

### [`pkg/services/reply/order_reply_test.go`](../../orchestrator-service/pkg/services/reply/order_reply_test.go)

Package `orchestrator-service/pkg/services/reply` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestHandlerClassifiesMalformedMessagesAsPermanent` | unit | PASS | – | Handler Classifies Malformed Messages As Permanent *(from the test name)* |
| `TestFinishedStepStatus` | unit | PASS | – | Finished Step Status *(from the test name)* |
| `TestIsPermanentDatabaseError` | unit | PASS | 6 | Is Permanent Database Error *(from the test name)* |

### [`pkg/statusstream/hub_test.go`](../../orchestrator-service/pkg/statusstream/hub_test.go)

Package `orchestrator-service/pkg/statusstream` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestHubNotifiesMatchingSubscribersAndCleansUp` | unit | PASS | – | Hub Notifies Matching Subscribers And Cleans Up *(from the test name)* |
| `TestHubEnforcesSubscriberLimit` | unit | PASS | – | Hub Enforces Subscriber Limit *(from the test name)* |
| `TestHubConcurrentSubscribeNotifyAndCancel` | unit | PASS | – | Hub Concurrent Subscribe Notify And Cancel *(from the test name)* |

### [`pkg/sweeper/sweeper_edge_integration_test.go`](../../orchestrator-service/pkg/sweeper/sweeper_edge_integration_test.go)

Package `orchestrator-service/pkg/sweeper` · 11 test function(s) · 11 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestStuckUndoSagaWithoutAResendPayloadIsNeverRevivedAndSpendsItsCounter` | integration (needs Postgres) | PASS | – | Stuck Undo Saga Without A Resend Payload Is Never Revived And Spends Its Counter *(from the test name)* |
| `TestAdminReviveRefusesWhenThereIsNothingToResendAndChangesNothing` | integration (needs Postgres) | PASS | – | Admin Revive Refuses When There Is Nothing To Resend And Changes Nothing *(from the test name)* |
| `TestAdminReviveOnlyTouchesStuckSagasAndPicksTheRightRunningStatus` | integration (needs Postgres) | PASS | – | Admin Revive Only Touches Stuck Sagas And Picks The Right Running Status *(from the test name)* |
| `TestRevivedUndoSagaIsActuallyResentByTheNextSweep` | integration (needs Postgres) | PASS | – | Revived Undo Saga Is Actually Resent By The Next Sweep *(from the test name)* |
| `TestTwoSweepersNeverResendTheSameStepTwice` | integration (needs Postgres) | PASS | – | Two Sweepers Never Resend The Same Step Twice *(from the test name)* |
| `TestTwoSweepersMarkAnExhaustedSagaStuckOnce` | integration (needs Postgres) | PASS | – | Two Sweepers Mark An Exhausted Saga Stuck Once *(from the test name)* |
| `TestResendDelayGrowsThenStopsAtTheCapWithBoundedJitter` | unit | PASS | – | Resend Delay Grows Then Stops At The Cap With Bounded Jitter *(from the test name)* |
| `TestStepResendAtTheBackoffCapNeverSchedulesBeyondTheCap` | integration (needs Postgres) | PASS | – | Step Resend At The Backoff Cap Never Schedules Beyond The Cap *(from the test name)* |
| `TestReservationSweeperReleasesOnlyCompensatedAndOrphanedReservations` | integration (needs Postgres) | PASS | – | Reservation Sweeper Releases Only Compensated And Orphaned Reservations *(from the test name)* |
| `TestReservationSweeperKeepsGoingAfterOneReleaseFailsAndReportsIt` | integration (needs Postgres) | PASS | – | Reservation Sweeper Keeps Going After One Release Fails And Reports It *(from the test name)* |
| `TestReservationSweeperIsIdempotentAcrossRepeatedSweeps` | integration (needs Postgres) | PASS | – | Reservation Sweeper Is Idempotent Across Repeated Sweeps *(from the test name)* |

### [`pkg/sweeper/sweeper_integration_test.go`](../../orchestrator-service/pkg/sweeper/sweeper_integration_test.go)

Package `orchestrator-service/pkg/sweeper` · 9 test function(s) · 9 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCreateOrderThatRunsOutOfAttemptsIsCompensatedNotStuck` | integration (needs Postgres) | PASS | – | Create Order That Runs Out Of Attempts Is Compensated Not Stuck *(from the test name)* |
| `TestCreateOrderPastItsDeadlineIsCompensatedAndLaterStepsAreStuck` | integration (needs Postgres) | PASS | – | Create Order Past Its Deadline Is Compensated And Later Steps Are Stuck *(from the test name)* |
| `TestWithoutAStepFailerTheOldBehaviourIsKept` | integration (needs Postgres) | PASS | – | Without A Step Failer The Old Behaviour Is Kept *(from the test name)* |
| `TestClaimOrphanClosesTheAttemptSoItsKeyAnswersCleanly` | integration (needs Postgres) | PASS | – | Claim Orphan Closes The Attempt So Its Key Answers Cleanly *(from the test name)* |
| `TestClaimOrphanLeavesALiveSagasReservationAlone` | integration (needs Postgres) | PASS | – | Claim Orphan Leaves A Live Sagas Reservation Alone *(from the test name)* |
| `TestClaimOrphanAllowsReleaseWhenNoSagaCanAppear` | integration (needs Postgres) | PASS | – | Claim Orphan Allows Release When No Saga Can Appear *(from the test name)* |
| `TestSagaStuckAfterPaymentIsRefundedOnlyAfterTheConfiguredTime` | integration (needs Postgres) | PASS | – | Saga Stuck After Payment Is Refunded Only After The Configured Time *(from the test name)* |
| `TestUnwindingAPaidCheckoutRunsRefundThenCancelThenReleaseAndALateReplyCannotCompleteIt` | integration (needs Postgres) | PASS | – | Unwinding A Paid Checkout Runs Refund Then Cancel Then Release And A Late Reply Cannot Complete It *(from the test name)* |
| `TestSagaStuckOnAnUndoStepIsRevivedAfterTheDelayAtMostThreeTimes` | integration (needs Postgres) | PASS | – | Saga Stuck On An Undo Step Is Revived After The Delay At Most Three Times *(from the test name)* |
