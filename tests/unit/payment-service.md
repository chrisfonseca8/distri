# Tests — `payment-service`

22 test files · 106 top-level test functions · 106 pass · 58 unit, 48 integration (need Postgres)

"Kind" is *integration* when the test is skipped in a plain `go test` run and only runs against a database (`DATABASE_TEST_URL` plus the matching `RUN_*_INTEGRATION=1` flag). "What it checks" is the comment above the test function; where there is none it is derived from the test name and marked so.

[← all test suites](README.md)

### [`cmd/payment-service/main_test.go`](../../payment-service/cmd/payment-service/main_test.go)

Package `payment-service/cmd/payment-service` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestParseRole` | unit | PASS | – | Parse Role *(from the test name)* |

### [`pkg/admin/review_integration_test.go`](../../payment-service/pkg/admin/review_integration_test.go)

Package `payment-service/pkg/admin` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRetryReviewRequiresANoteAndRefusesRecordsHoldingAProviderRefund` | integration (needs Postgres) | PASS | – | Retry Review Requires A Note And Refuses Records Holding A Provider Refund *(from the test name)* |
| `TestParkedEventsAreListedAndReplayedOnce` | integration (needs Postgres) | PASS | – | Parked Events Are Listed And Replayed Once *(from the test name)* |

### [`pkg/config/webhook_config_test.go`](../../payment-service/pkg/config/webhook_config_test.go)

Package `payment-service/pkg/config` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRazorpayWithoutWebhookSecretRefusesToStartUnlessDisabledByName` | unit | PASS | – | Razorpay Without Webhook Secret Refuses To Start Unless Disabled By Name *(from the test name)* |
| `TestOtherProvidersNeverNeedAWebhookSecret` | unit | PASS | – | Other Providers Never Need A Webhook Secret *(from the test name)* |

### [`pkg/config/worker_config_test.go`](../../payment-service/pkg/config/worker_config_test.go)

Package `payment-service/pkg/config` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestLoadWorkerConfigDefaults` | unit | PASS | – | Load Worker Config Defaults *(from the test name)* |
| `TestWorkerConfigRejectsHandlerTimeoutAtLeastLease` | unit | PASS | – | Worker Config Rejects Handler Timeout At Least Lease *(from the test name)* |

### [`pkg/jobs/backoff_test.go`](../../payment-service/pkg/jobs/backoff_test.go)

Package `payment-service/pkg/jobs` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRetryBackoffGrowthJitterAndStrictCap` | unit | PASS | – | Retry Backoff Growth Jitter And Strict Cap *(from the test name)* |

### [`pkg/jobs/store_integration_test.go`](../../payment-service/pkg/jobs/store_integration_test.go)

Package `payment-service/pkg/jobs` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestClaimConcurrentWorkers` | integration (needs Postgres) | PASS | – | Claim Concurrent Workers *(from the test name)* |

### [`pkg/jobs/worker_test.go`](../../payment-service/pkg/jobs/worker_test.go)

Package `payment-service/pkg/jobs` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestWorkerMapsPaymentOutcomes` | unit | PASS | 4 | Worker Maps Payment Outcomes *(from the test name)* |
| `TestWorkerRecoversPaymentHandlerPanic` | unit | PASS | – | Worker Recovers Payment Handler Panic *(from the test name)* |
| `TestWorkerDrainsClaimedJobAfterShutdownSignal` | unit | PASS | – | Worker Drains Claimed Job After Shutdown Signal *(from the test name)* |

### [`pkg/observability/middleware_test.go`](../../payment-service/pkg/observability/middleware_test.go)

Package `payment-service/pkg/observability` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCorrelationSurvivesHTTPAndAMQP` | unit | PASS | – | Correlation Survives HTTP And AMQP *(from the test name)* |
| `TestLoggerAddsCorrelationAndRedactsSecrets` | unit | PASS | – | Logger Adds Correlation And Redacts Secrets *(from the test name)* |
| `TestCorrelationJSONRoundTrip` | unit | PASS | – | Correlation JSON Round Trip *(from the test name)* |

### [`pkg/payment_gateway/razorpay_test.go`](../../payment-service/pkg/payment_gateway/razorpay_test.go)

Package `payment-service/pkg/payment_gateway` · 11 test function(s) · 11 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestNewRazorpayValidatesCredentials` | unit | PASS | – | New Razorpay Validates Credentials *(from the test name)* |
| `TestCreateChargeCreatesAnOrderThatAwaitsPayment` | unit | PASS | – | Create Charge Creates An Order That Awaits Payment *(from the test name)* |
| `TestCreateChargeClassifiesFailures` | unit | PASS | 5 | Create Charge Classifies Failures *(from the test name)* |
| `TestCreateChargeTimeoutIsAmbiguous` | unit | PASS | – | Create Charge Timeout Is Ambiguous *(from the test name)* |
| `TestCreateChargeDuplicateReceiptReturnsTheExistingOrder` | unit | PASS | – | Create Charge Duplicate Receipt Returns The Existing Order *(from the test name)* |
| `TestCreateChargeDuplicateReceiptWithAnotherAmountIsRejected` | unit | PASS | – | Create Charge Duplicate Receipt With Another Amount Is Rejected *(from the test name)* |
| `TestLookupOrder` | unit | PASS | 4 | Lookup Order *(from the test name)* |
| `TestRefund` | unit | PASS | 5 | Refund *(from the test name)* |
| `TestRefundFetchListAndPaymentLookup` | unit | PASS | – | Refund Fetch List And Payment Lookup *(from the test name)* |
| `TestLookupOrderToleratesSeveralOrdersWithOneReceipt` | unit | PASS | – | Lookup Order Tolerates Several Orders With One Receipt *(from the test name)* |
| `TestGatewayNameSeparatesSimulatedFromRealEndpoints` | unit | PASS | – | Gateway Name Separates Simulated From Real Endpoints *(from the test name)* |

### [`pkg/payment_gateway/testGateway_test.go`](../../payment-service/pkg/payment_gateway/testGateway_test.go)

Package `payment-service/pkg/payment_gateway` · 6 test function(s) · 6 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestSimulatorSuccessRange` | unit | PASS | – | Simulator Success Range *(from the test name)* |
| `TestSimulatorDeclineRange` | unit | PASS | – | Simulator Decline Range *(from the test name)* |
| `TestSimulatorTimeoutRangeWaitsForContext` | unit | PASS | – | Simulator Timeout Range Waits For Context *(from the test name)* |
| `TestSimulatorHonorsCancellationDuringLatency` | unit | PASS | – | Simulator Honors Cancellation During Latency *(from the test name)* |
| `TestNewSimulatorFromEnv` | unit | PASS | – | New Simulator From Env *(from the test name)* |
| `TestNewSimulatorDefaultsToSuccess` | unit | PASS | – | New Simulator Defaults To Success *(from the test name)* |

### [`pkg/retention/retention_integration_test.go`](../../payment-service/pkg/retention/retention_integration_test.go)

Package `payment-service/pkg/retention` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPurgeKeepsFinancialRecordsAndUnfinishedWork` | integration (needs Postgres) | PASS | – | Purge Keeps Financial Records And Unfinished Work *(from the test name)* |

### [`pkg/services/compensation_worker_test.go`](../../payment-service/pkg/services/compensation_worker_test.go)

Package `payment-service/pkg/services` · 11 test function(s) · 11 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestLateCaptureBecomesAConfirmedProcessedRefund` | integration (needs Postgres) | PASS | – | Late Capture Becomes A Confirmed Processed Refund *(from the test name)* |
| `TestPendingRefundStaysUnresolvedUntilProcessed` | integration (needs Postgres) | PASS | – | Pending Refund Stays Unresolved Until Processed *(from the test name)* |
| `TestLostSuccessfulResponseIsReconciledWithoutAnotherSubmission` | integration (needs Postgres) | PASS | – | Lost Successful Response Is Reconciled Without Another Submission *(from the test name)* |
| `TestWorkerStoppingAtAnyPointRecoversSafely` | unit | PASS | 3 | Worker Stopping At Any Point Recovers Safely *(from the test name)* |
| `TestUnknownOutcomeIsNeverReadAsNoRefund` | unit | PASS | 3 | Unknown Outcome Is Never Read As No Refund *(from the test name)* |
| `TestDefinitiveFailureFollowsTheRecoveryPolicy` | unit | PASS | 2 | Definitive Failure Follows The Recovery Policy *(from the test name)* |
| `TestConcurrentWorkersCannotOverRefund` | integration (needs Postgres) | PASS | – | Concurrent Workers Cannot Over Refund *(from the test name)* |
| `TestOnlyEstablishedRecordsAreRefunded` | unit | PASS | 3 | Only Established Records Are Refunded *(from the test name)* |
| `TestRefundWebhooksAndPollingCauseOneBusinessEffect` | integration (needs Postgres) | PASS | 1 | Refund Webhooks And Polling Cause One Business Effect *(from the test name)* |
| `TestGatewayEnvironmentsNeverCrossRoutes` | integration (needs Postgres) | PASS | 2 | Gateway Environments Never Cross Routes *(from the test name)* |
| `TestSettledAttemptAuditFindsAnExtraCaptureOnlyInsideTheWindow` | integration (needs Postgres) | PASS | – | Settled Attempt Audit Finds An Extra Capture Only Inside The Window *(from the test name)* |

### [`pkg/services/payment_flow_test.go`](../../payment-service/pkg/services/payment_flow_test.go)

Package `payment-service/pkg/services` · 8 test function(s) · 8 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPaymentFlowOutcomes` | integration (needs Postgres) | PASS | 3 | Payment Flow Outcomes *(from the test name)* |
| `TestReclaimedPendingPaymentBecomesUnknownWithoutGatewayCall` | integration (needs Postgres) | PASS | – | Reclaimed Pending Payment Becomes Unknown Without Gateway Call *(from the test name)* |
| `TestSuccessfulGatewayResultUsesFreshWriteContext` | integration (needs Postgres) | PASS | – | Successful Gateway Result Uses Fresh Write Context *(from the test name)* |
| `TestRepeatedCreateReturnsSameAttemptAndOneProcessJob` | integration (needs Postgres) | PASS | – | Repeated Create Returns Same Attempt And One Process Job *(from the test name)* |
| `TestConcurrentCreateYieldsOnePaymentAndOneProcessJob` | integration (needs Postgres) | PASS | – | Concurrent Create Yields One Payment And One Process Job *(from the test name)* |
| `TestProcessingFinalPaymentDoesNotCallGatewayAgain` | integration (needs Postgres) | PASS | – | Processing Final Payment Does Not Call Gateway Again *(from the test name)* |
| `TestCreatePaymentValidation` | unit | PASS | 3 | Create Payment Validation *(from the test name)* |
| `TestJobHandlerNeverCallsGatewayForAnotherEnvironmentsAttempt` | integration (needs Postgres) | PASS | – | Job Handler Never Calls Gateway For Another Environments Attempt *(from the test name)* |

### [`pkg/services/payment_initiate_integration_test.go`](../../payment-service/pkg/services/payment_initiate_integration_test.go)

Package `payment-service/pkg/services` · 5 test function(s) · 5 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestSameInitiateMessageRedeliveredManyTimesCreatesOnePaymentAndOneJob` | integration (needs Postgres) | PASS | – | Same Initiate Message Redelivered Many Times Creates One Payment And One Job *(from the test name)* |
| `TestSameInitiateMessageDeliveredConcurrentlyCreatesOnePayment` | integration (needs Postgres) | PASS | – | Same Initiate Message Delivered Concurrently Creates One Payment *(from the test name)* |
| `TestResentInitiateWithANewMessageIdStillCreatesOnePayment` | integration (needs Postgres) | PASS | – | Resent Initiate With A New Message Id Still Creates One Payment *(from the test name)* |
| `TestResentInitiateForAnAlreadyFinishedPaymentRepeatsTheReplyButNeverTheCharge` | integration (needs Postgres) | PASS | – | Resent Initiate For An Already Finished Payment Repeats The Reply But Never The Charge *(from the test name)* |
| `TestInvalidInitiateCommandsAreRejectedPermanentlyAndLeaveNoTrace` | integration (needs Postgres) | PASS | – | Invalid Initiate Commands Are Rejected Permanently And Leave No Trace *(from the test name)* |

### [`pkg/services/payment_processor_test.go`](../../payment-service/pkg/services/payment_processor_test.go)

Package `payment-service/pkg/services` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPaymentResultUpdates` | unit | PASS | 3 | Payment Result Updates *(from the test name)* |

### [`pkg/services/payment_reconciler_test.go`](../../payment-service/pkg/services/payment_reconciler_test.go)

Package `payment-service/pkg/services` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPaymentResultUpdatesForAsyncGateway` | unit | PASS | – | Payment Result Updates For Async Gateway *(from the test name)* |
| `TestDecideReconcile` | unit | PASS | 23 | Decide Reconcile *(from the test name)* |

### [`pkg/services/refund_after_payment_integration_test.go`](../../payment-service/pkg/services/refund_after_payment_integration_test.go)

Package `payment-service/pkg/services` · 5 test function(s) · 5 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRefundCommandRecordsTheFundingPaymentOnceAndTheWorkerRefundsItExactlyOnce` | integration (needs Postgres) | PASS | – | The saga asks for the refund of the payment that funded a checkout it could not complete. |
| `TestTheFundingPaymentIsNeverRefundedForAnyOtherReason` | integration (needs Postgres) | PASS | – | Only the saga's explicit request lets the funding payment be refunded: a polling finding never does. |
| `TestRefundCommandAnswersByWhatIsKnownAboutThePayment` | integration (needs Postgres) | PASS | 3 | Refund Command Answers By What Is Known About The Payment *(from the test name)* |
| `TestTheDispatcherRoutesByRoutingKeyAndRejectsOthers` | unit | PASS | – | The Dispatcher Routes By Routing Key And Rejects Others *(from the test name)* |
| `TestRejectedRefundInReviewIsRetriedAutomaticallyAFewTimesThenLeftToAPerson` | integration (needs Postgres) | PASS | – | Rejected Refund In Review Is Retried Automatically A Few Times Then Left To A Person *(from the test name)* |

### [`pkg/services/unconfirmed_integration_test.go`](../../payment-service/pkg/services/unconfirmed_integration_test.go)

Package `payment-service/pkg/services` · 6 test function(s) · 6 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestUnreachableGatewayPastWindowPlusGraceFailsTheAttempt` | integration (needs Postgres) | PASS | – | IMP-6: when the gateway cannot be asked for longer than the window plus the grace, the payment service decides FAILED (UNCONFIRMED) and tells the saga. |
| `TestUnreachableGatewayInsideTheGraceKeepsAskingAndNeverGuesses` | integration (needs Postgres) | PASS | – | Unreachable Gateway Inside The Grace Keeps Asking And Never Guesses *(from the test name)* |
| `TestUnconfirmedRuleCanBeSwitchedOff` | integration (needs Postgres) | PASS | – | Unconfirmed Rule Can Be Switched Off *(from the test name)* |
| `TestCaptureFoundAfterAnUnconfirmedFailureIsRefundedNotAccepted` | integration (needs Postgres) | PASS | – | A capture that turns up after the attempt was failed as UNCONFIRMED is refunded, never accepted. |
| `TestConnectionRefusedIsNotSentButATimeoutAfterSendingIsNot` | unit | PASS | – | Connection Refused Is Not Sent But A Timeout After Sending Is Not *(from the test name)* |
| `TestNotSentRequestIsRetriedAndLeavesTheAttemptCreated` | integration (needs Postgres) | PASS | – | Not Sent Request Is Retried And Leaves The Attempt Created *(from the test name)* |

### [`pkg/services/webhook_processor_edge_integration_test.go`](../../payment-service/pkg/services/webhook_processor_edge_integration_test.go)

Package `payment-service/pkg/services` · 8 test function(s) · 8 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestDecideReconcileTreatsTheExactDeadlineInstantAsLate` | unit | PASS | – | Decide Reconcile Treats The Exact Deadline Instant As Late *(from the test name)* |
| `TestDecideReconcileWithNoGatewayOrderExpiresExactlyAtTheDeadline` | unit | PASS | – | Decide Reconcile With No Gateway Order Expires Exactly At The Deadline *(from the test name)* |
| `TestDecideReconcileFallsBackToCreationTimePlusWindowWhenThereIsNoExpiry` | unit | PASS | – | Decide Reconcile Falls Back To Creation Time Plus Window When There Is No Expiry *(from the test name)* |
| `TestWebhookStormAcrossManyPaymentsAppliesEachCaptureExactlyOnce` | integration (needs Postgres) | PASS | – | Webhook Storm Across Many Payments Applies Each Capture Exactly Once *(from the test name)* |
| `TestWebhookStormOnOneOrderWithFourProcessorsCreatesOneReply` | integration (needs Postgres) | PASS | – | Webhook Storm On One Order With Four Processors Creates One Reply *(from the test name)* |
| `TestReplayingProcessedEventsChangesNothing` | integration (needs Postgres) | PASS | – | Replaying Processed Events Changes Nothing *(from the test name)* |
| `TestRepeatedCapturesOnACancelledCheckoutOweOneRefundPerPayment` | integration (needs Postgres) | PASS | – | Repeated Captures On A Cancelled Checkout Owe One Refund Per Payment *(from the test name)* |
| `TestCaptureForAnAmountThatDoesNotMatchIsNeverAcceptedEvenInAStorm` | integration (needs Postgres) | PASS | – | Capture For An Amount That Does Not Match Is Never Accepted Even In A Storm *(from the test name)* |

### [`pkg/services/webhook_processor_integration_test.go`](../../payment-service/pkg/services/webhook_processor_integration_test.go)

Package `payment-service/pkg/services` · 15 test function(s) · 15 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCapturedWebhookCompletesThePaymentOnceUnderConcurrentDiscovery` | integration (needs Postgres) | PASS | – | Captured Webhook Completes The Payment Once Under Concurrent Discovery *(from the test name)* |
| `TestOlderAuthorizedEventDoesNotRegressACapturedPayment` | integration (needs Postgres) | PASS | – | Older Authorized Event Does Not Regress A Captured Payment *(from the test name)* |
| `TestFailedPaymentAttemptDoesNotFailTheCheckout` | integration (needs Postgres) | PASS | – | Failed Payment Attempt Does Not Fail The Checkout *(from the test name)* |
| `TestEventBeforeTheGatewayOrderMappingIsRetriedNotDiscarded` | integration (needs Postgres) | PASS | – | Event Before The Gateway Order Mapping Is Retried Not Discarded *(from the test name)* |
| `TestEventThatNeverMapsIsParkedAfterBoundedRetries` | integration (needs Postgres) | PASS | – | Event That Never Maps Is Parked After Bounded Retries *(from the test name)* |
| `TestDeadlinePolicyUsesOurObservationTimeNotProviderTimestamps` | unit | PASS | 3 | Deadline Policy Uses Our Observation Time Not Provider Timestamps *(from the test name)* |
| `TestCaptureNotifiedAfterCancellationDoesNotResurrectAndIsTracked` | integration (needs Postgres) | PASS | – | Capture Notified After Cancellation Does Not Resurrect And Is Tracked *(from the test name)* |
| `TestAdditionalCapturesGetTheirOwnCompensation` | unit | PASS | 2 | Additional Captures Get Their Own Compensation *(from the test name)* |
| `TestParkedEventIsReplayedAfterTheMappingAppears` | integration (needs Postgres) | PASS | – | Parked Event Is Replayed After The Mapping Appears *(from the test name)* |
| `TestOnlyNoMappingParkedEventsAreReplayed` | integration (needs Postgres) | PASS | – | Only No Mapping Parked Events Are Replayed *(from the test name)* |
| `TestExpiryAndWebhookRaceLeaveOneConsistentOutcome` | integration (needs Postgres) | PASS | – | The expiry decision and the webhook worker race for the same attempt, started together behind a barrier. Whoever commits first wins, and the other must leave one consistent outcome: never two replies, never captured money without a record. |
| `TestExpiryIsDeferredWhileAnOnTimeCaptureWaitsInTheInbox` | integration (needs Postgres) | PASS | – | Expiry Is Deferred While An On Time Capture Waits In The Inbox *(from the test name)* |
| `TestCapturedAmountOrCurrencyMismatchIsNotAccepted` | unit | PASS | 2 | Captured Amount Or Currency Mismatch Is Not Accepted *(from the test name)* |
| `TestProcessedMarkAndEffectsCommitTogether` | integration (needs Postgres) | PASS | – | Processed Mark And Effects Commit Together *(from the test name)* |
| `TestExpiryAndWebhookInEitherOrderAreConsistent` | unit | PASS | 2 | Both interleavings of the same race, forced explicitly rather than hoped for: the decision to expire is taken from a stale read in each case. |

### [`pkg/services/webhook_processor_loop_test.go`](../../payment-service/pkg/services/webhook_processor_loop_test.go)

Package `payment-service/pkg/services` · 4 test function(s) · 4 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestWebhookProcessorDrainsConsecutiveBatchesWithoutIdleDelay` | unit | PASS | – | Webhook Processor Drains Consecutive Batches Without Idle Delay *(from the test name)* |
| `TestWebhookProcessorWaitsWhenInboxIsIdle` | unit | PASS | – | Webhook Processor Waits When Inbox Is Idle *(from the test name)* |
| `TestNextWebhookErrorDelayBacksOffAndCaps` | unit | PASS | – | Next Webhook Error Delay Backs Off And Caps *(from the test name)* |
| `TestWaitForWebhookPassStopsPromptlyOnCancellation` | unit | PASS | – | Wait For Webhook Pass Stops Promptly On Cancellation *(from the test name)* |

### [`pkg/webhooks/handler_test.go`](../../payment-service/pkg/webhooks/handler_test.go)

Package `payment-service/pkg/webhooks` · 8 test function(s) · 8 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestVerifySignature` | unit | PASS | 7 | Verify Signature *(from the test name)* |
| `TestInvalidSignatureChangesNothing` | unit | PASS | – | Invalid Signature Changes Nothing *(from the test name)* |
| `TestAcceptedEventIsRecordedExactly` | unit | PASS | – | Accepted Event Is Recorded Exactly *(from the test name)* |
| `TestDuplicateDeliveryIsAcknowledgedWithoutASecondRecord` | unit | PASS | – | Duplicate Delivery Is Acknowledged Without A Second Record *(from the test name)* |
| `TestUnsupportedEventsAreAcknowledgedAndIgnoredDeliberately` | unit | PASS | – | Unsupported Events Are Acknowledged And Ignored Deliberately *(from the test name)* |
| `TestDurableAcceptanceFailureIsRetryable` | unit | PASS | – | Durable Acceptance Failure Is Retryable *(from the test name)* |
| `TestRequestValidation` | unit | PASS | – | Request Validation *(from the test name)* |
| `TestNewHandlerRequiresASecret` | unit | PASS | – | New Handler Requires A Secret *(from the test name)* |
