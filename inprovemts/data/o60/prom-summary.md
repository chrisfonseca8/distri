# Prometheus timeline summary — c4-rate-060

whole run 2026-11-09T15:20:51.660128672Z .. 2026-11-09T15:25:18.559994858Z; measurement window = +0s .. +180s of the run.


## RabbitMQ queues (max over whole run + 2 min, mean over measurement window)

| queue | kind | max | mean in window |
|---|---|---|---|
| distri.inventory.commands | ready | 2.00 | 0.00 |
| distri.orchestrator.replies | ready | 135 | 6.84 |
| distri.order.commands | ready | 38.00 | 2.76 |
| distri.inventory.commands | unacked | 10.00 | 0.95 |
| distri.orchestrator.replies | unacked | 40.00 | 10.54 |
| distri.order.commands | unacked | 13.00 | 3.03 |
| distri.payment.commands | unacked | 8.00 | 1.65 |

## Consumers connected (autoscale), max

| queue | max |
|---|---|
| distri.inventory.commands | 2.00 |
| distri.orchestrator.replies | 9.00 |
| distri.order.commands | 4.00 |
| distri.payment.commands | 2.00 |

## RabbitMQ consumers (measurement window)

| queue | acked/s | handler p95 s |
|---|---|---|
| distri.inventory.commands | 42.07 | 0.02 |
| distri.orchestrator.replies | 187 | 0.03 |
| distri.order.commands | 102 | 0.02 |
| distri.payment.commands | 65.45 | 0.02 |

## Dead letters during run

| queue | reason | count |
|---|---|---|
| none |  | 0 |

## Retry-queue republishes during run

| queue | count |
|---|---|
| none | 0 |

## Backlogs / ages (max over whole run + 2 min)

| metric | service | max |
|---|---|---|
| outbox pending rows | orchestrator-service | 160 |
| outbox pending rows | payment-service | 24.00 |
| outbox pending rows | inventory-service | 30.00 |
| outbox pending rows | order-service | 12.00 |
| outbox oldest pending age s | orchestrator-service | 0.65 |
| outbox oldest pending age s | payment-service | 0.50 |
| outbox oldest pending age s | inventory-service | 0.43 |
| outbox oldest pending age s | order-service | 0.09 |
| payment webhook backlog | payment-service | 9005 |
| payment webhook oldest pending age s | payment-service | 76.73 |
| payment jobs by status (queued) | payment-service | 14.00 |
| payment jobs (running) | payment-service | 3.00 |
| payment jobs oldest queued age s | payment-service | 0.16 |
| saga oldest active age s | orchestrator-service | 88.37 |
| sagas RUNNING | orchestrator-service | 3380 |
| outbox dead rows | orchestrator-service | 0.00 |
| outbox dead rows | payment-service | 0.00 |
| outbox dead rows | inventory-service | 0.00 |
| outbox dead rows | order-service | 0.00 |

## Running sagas by current step (max)

| current_step | max |
|---|---|
| - | 58.00 |
| - | 66.00 |
| - | 33.00 |
| - | 3361 |

## CPU in measurement window (container % of one core; host % of 2 vCPU; 'time >70%' = seconds the container exceeded 70% of one core)

| service | container mean | container max | time >70% of a core | host mean | host max |
|---|---|---|---|---|---|
| orchestrator | 59.47 | 84.33 | 55s | 41.88 | 57.08 |
| payment | 22.20 | 24.59 | 0s | 17.07 | 18.64 |
| inventory | 13.78 | 15.54 | 0s | 10.71 | 12.02 |
| order | 11.25 | 12.26 | 0s | 8.89 | 9.66 |
| broker (host) | - | - | - | 17.57 | 19.58 |

## Saga step duration p95 (orchestrator metric)

| step | n | p95 s |
|---|---|---|
| CONFIRM_STOCK | 7593 | 0.93 |
| CREATE_ORDER | 10795 | 0.76 |
| MARK_ORDER_PAID | 7599 | 0.74 |
| TAKE_PAYMENT | 7599 | 57.18 |

## Saga end-to-end duration p95

| status | n | p95 s |
|---|---|---|
| COMPLETED | 7594 | 57.33 |

## HTTP handler p95 by service/route (inbound)

| service | route | n | p95 s |
|---|---|---|---|
| inventory-service | GET /internal/v1/reservations | 3.09 | 0.00 |
| inventory-service | POST /internal/v1/reservations | 10799 | 0.02 |
| orchestrator-service | GET /v1/checkouts/{orderID} | 170835 | 0.05 |
| orchestrator-service | POST /v1/checkouts | 10798 | 0.09 |
| payment-service | POST /v1/webhooks/razorpay | 32082 | 0.01 |

## HTTP client call p95 (outbound from service to peer)

| service | peer | n | p95 s |
|---|---|---|---|
| orchestrator-service | inventory-service | 10804 | 0.02 |

## DB statement p95 by service

| service | n | p95 s |
|---|---|---|
| inventory-service | 119201 | 0.00 |
| orchestrator-service | 694644 | 0.01 |
| order-service | 115963 | 0.00 |
| payment-service | 214079 | 0.00 |

## Outbox: row created -> confirmed publish p95

| service | n | p95 s |
|---|---|---|
| inventory-service | 7577 | 0.48 |
| orchestrator-service | 37780 | 0.17 |
| order-service | 18392 | 0.09 |
| payment-service | 7732 | 0.49 |

## Payment gateway call p95

| service | n | p95 s |
|---|---|---|
| payment-service | 11282 | 0.01 |

## Payment webhook processing p95

| service | n | p95 s |
|---|---|---|
| payment-service | 22828 | 0.02 |

## DB transactions in window

| service | name | result | count |
|---|---|---|---|
| orchestrator-service | saga_reply | committed | 33719 |
| payment-service | payment_initiate | committed | 11780 |
| inventory-service | create_reservation | committed | 10799 |
| orchestrator-service | create_saga | committed | 10799 |
| order-service | order_create | committed | 10799 |
| order-service | order_transition | committed | 7599 |
| inventory-service | inventory_CONFIRMED | committed | 7573 |
| orchestrator-service | saga_sweep_step | committed | 1014 |
