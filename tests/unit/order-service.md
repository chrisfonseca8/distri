# Tests — `order-service`

11 test files · 31 top-level test functions · 31 pass · 23 unit, 8 integration (need Postgres)

"Kind" is *integration* when the test is skipped in a plain `go test` run and only runs against a database (`DATABASE_TEST_URL` plus the matching `RUN_*_INTEGRATION=1` flag). "What it checks" is the comment above the test function; where there is none it is derived from the test name and marked so.

[← all test suites](README.md)

### [`cmd/order-service/main_test.go`](../../order-service/cmd/order-service/main_test.go)

Package `order-service/cmd/order-service` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestParseRole` | unit | PASS | – | Parse Role *(from the test name)* |

### [`pkg/config/outbox_config_test.go`](../../order-service/pkg/config/outbox_config_test.go)

Package `order-service/pkg/config` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestLoadOutboxConcurrency` | unit | PASS | – | Load Outbox Concurrency *(from the test name)* |
| `TestLoadOutboxPollInterval` | unit | PASS | – | Load Outbox Poll Interval *(from the test name)* |

### [`pkg/config/rabbitmq_config_test.go`](../../order-service/pkg/config/rabbitmq_config_test.go)

Package `order-service/pkg/config` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestLoadRabbitMQConfigDefaultsConsumerConcurrency` | unit | PASS | – | Load Rabbit MQ Config Defaults Consumer Concurrency *(from the test name)* |
| `TestLoadRabbitMQConfigReadsConsumerConcurrency` | unit | PASS | – | Load Rabbit MQ Config Reads Consumer Concurrency *(from the test name)* |
| `TestLoadRabbitMQConfigRejectsInvalidConsumerConcurrency` | unit | PASS | 3 | Load Rabbit MQ Config Rejects Invalid Consumer Concurrency *(from the test name)* |

### [`pkg/messaging/consumer_test.go`](../../order-service/pkg/messaging/consumer_test.go)

Package `order-service/pkg/messaging` · 4 test function(s) · 4 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestConsumerAcknowledgesSuccessfulDelivery` | unit | PASS | – | Consumer Acknowledges Successful Delivery *(from the test name)* |
| `TestConsumerDeadLettersPermanentFailure` | unit | PASS | – | Consumer Dead Letters Permanent Failure *(from the test name)* |
| `TestConsumerRequeuesTransientFailureWhenRetryRepublishFails` | unit | PASS | – | Consumer Requeues Transient Failure When Retry Republish Fails *(from the test name)* |
| `TestNewConsumerValidatesRequiredValues` | unit | PASS | – | New Consumer Validates Required Values *(from the test name)* |

### [`pkg/observability/middleware_test.go`](../../order-service/pkg/observability/middleware_test.go)

Package `order-service/pkg/observability` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCorrelationSurvivesHTTPAndAMQP` | unit | PASS | – | Correlation Survives HTTP And AMQP *(from the test name)* |
| `TestLoggerAddsCorrelationAndRedactsSecrets` | unit | PASS | – | Logger Adds Correlation And Redacts Secrets *(from the test name)* |
| `TestCorrelationJSONRoundTrip` | unit | PASS | – | Correlation JSON Round Trip *(from the test name)* |

### [`pkg/outbox/relay_test.go`](../../order-service/pkg/outbox/relay_test.go)

Package `order-service/pkg/outbox` · 6 test function(s) · 6 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRelayMarksConfirmedMessagePublished` | unit | PASS | – | Relay Marks Confirmed Message Published *(from the test name)* |
| `TestRelayReschedulesFailedPublish` | unit | PASS | – | Relay Reschedules Failed Publish *(from the test name)* |
| `TestPoisonMessageBecomesDeadButBrokerOutageDoesNotCount` | unit | PASS | – | Poison Message Becomes Dead But Broker Outage Does Not Count *(from the test name)* |
| `TestRetryDelayCapsExponentialBackoff` | unit | PASS | – | Retry Delay Caps Exponential Backoff *(from the test name)* |
| `TestRelayReleasesUnpublishedBatchOnShutdown` | unit | PASS | – | Relay Releases Unpublished Batch On Shutdown *(from the test name)* |
| `TestRelayConfigRequiresLeaseForWholeBatch` | unit | PASS | – | Relay Config Requires Lease For Whole Batch *(from the test name)* |

### [`pkg/outbox/store_test.go`](../../order-service/pkg/outbox/store_test.go)

Package `order-service/pkg/outbox` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestEnqueueValidatesMessage` | unit | PASS | 3 | Enqueue Validates Message *(from the test name)* |

### [`pkg/retention/retention_integration_test.go`](../../order-service/pkg/retention/retention_integration_test.go)

Package `order-service/pkg/retention` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPurgeRemovesOnlyExpiredMessageBookkeeping` | integration (needs Postgres) | PASS | – | Purge Removes Only Expired Message Bookkeeping *(from the test name)* |

### [`pkg/services/order_create_integration_test.go`](../../order-service/pkg/services/order_create_integration_test.go)

Package `order-service/pkg/services` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestOrderCreateHandlerTransactionAndIdempotency` | integration (needs Postgres) | PASS | – | Order Create Handler Transaction And Idempotency *(from the test name)* |
| `TestOrderCreateHandlerReturnsTransientDatabaseErrors` | integration (needs Postgres) | PASS | – | Order Create Handler Returns Transient Database Errors *(from the test name)* |

### [`pkg/services/order_create_test.go`](../../order-service/pkg/services/order_create_test.go)

Package `order-service/pkg/services` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestValidateOrderCreate` | unit | PASS | 8 | Validate Order Create *(from the test name)* |
| `TestOrderCreateHandlerClassifiesInvalidMessagesAsPermanent` | unit | PASS | – | Order Create Handler Classifies Invalid Messages As Permanent *(from the test name)* |
| `TestIsPermanentDatabaseError` | unit | PASS | 6 | Is Permanent Database Error *(from the test name)* |

### [`pkg/services/order_failure_integration_test.go`](../../order-service/pkg/services/order_failure_integration_test.go)

Package `order-service/pkg/services` · 5 test function(s) · 5 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestInvalidOrderCreateIsAnsweredFailedSoTheSagaCanCompensate` | integration (needs Postgres) | PASS | – | EDGE-11 / IMP-1: a command that can never succeed is answered Failed, not dead-lettered silently. |
| `TestCancelBeforeCreateLeavesATombstoneAndTheLateCreateIsRefused` | integration (needs Postgres) | PASS | – | EDGE-13 / IMP-2: cancel before create leaves a tombstone, and the late create does nothing. |
| `TestMarkPaidForAnOrderThatWasNeverCreatedIsAnsweredFailed` | integration (needs Postgres) | PASS | – | Mark Paid For An Order That Was Never Created Is Answered Failed *(from the test name)* |
| `TestExistingOrderWithDifferentContentsIsAnsweredFailed` | integration (needs Postgres) | PASS | – | EDGE-12 / IMP-3: an order that already exists with different contents is not silently accepted. |
| `TestPaidOrderIsCancelledOnlyWhenItsPaymentIsBeingRefunded` | integration (needs Postgres) | PASS | – | A paid order is cancelled only by the cancel of a refunded checkout. |
