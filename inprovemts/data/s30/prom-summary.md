# Prometheus timeline summary — c4-rate-030

whole run 2026-11-09T15:06:32.589208253Z .. 2026-11-09T15:09:49.23397461Z; measurement window = +0s .. +180s of the run.


## RabbitMQ queues (max over whole run + 2 min, mean over measurement window)

| queue | kind | max | mean in window |
|---|---|---|---|
| distri.inventory.commands | ready | 3.00 | 0.00 |
| distri.orchestrator.replies | ready | 43.00 | 1.16 |
| distri.inventory.commands | unacked | 10.00 | 0.54 |
| distri.orchestrator.replies | unacked | 20.00 | 3.19 |
| distri.order.commands | unacked | 11.00 | 1.30 |
| distri.payment.commands | unacked | 3.00 | 0.46 |

## Consumers connected (autoscale), max

| queue | max |
|---|---|
| distri.inventory.commands | 3.00 |
| distri.orchestrator.replies | 8.00 |
| distri.order.commands | 2.00 |
| distri.payment.commands | 1.00 |

## RabbitMQ consumers (measurement window)

| queue | acked/s | handler p95 s |
|---|---|---|
| distri.inventory.commands | 29.46 | 0.02 |
| distri.orchestrator.replies | 119 | 0.02 |
| distri.order.commands | 59.57 | 0.02 |
| distri.payment.commands | 30.01 | 0.02 |

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
| outbox pending rows | orchestrator-service | 22.00 |
| outbox pending rows | payment-service | 19.00 |
| outbox pending rows | inventory-service | 20.00 |
| outbox pending rows | order-service | 10.00 |
| outbox oldest pending age s | orchestrator-service | 0.09 |
| outbox oldest pending age s | payment-service | 0.48 |
| outbox oldest pending age s | inventory-service | 0.47 |
| outbox oldest pending age s | order-service | 0.08 |
| payment webhook backlog | payment-service | 949 |
| payment webhook oldest pending age s | payment-service | 8.45 |
| payment jobs by status (queued) | payment-service | 6.00 |
| payment jobs (running) | payment-service | 2.00 |
| payment jobs oldest queued age s | payment-service | 0.14 |
| saga oldest active age s | orchestrator-service | 29.73 |
| sagas RUNNING | orchestrator-service | 481 |
| outbox dead rows | orchestrator-service | 0.00 |
| outbox dead rows | payment-service | 0.00 |
| outbox dead rows | inventory-service | 0.00 |
| outbox dead rows | order-service | 0.00 |

## Running sagas by current step (max)

| current_step | max |
|---|---|
| - | 26.00 |
| - | 18.00 |
| - | 25.00 |
| - | 466 |

## CPU in measurement window (container % of one core; host % of 2 vCPU; 'time >70%' = seconds the container exceeded 70% of one core)

| service | container mean | container max | time >70% of a core | host mean | host max |
|---|---|---|---|---|---|
| orchestrator | 25.85 | 28.45 | 0s | 20.44 | 23.16 |
| payment | 14.24 | 15.42 | 0s | 11.83 | 12.84 |
| inventory | 8.73 | 10.12 | 0s | 7.87 | 8.84 |
| order | 7.86 | 8.64 | 0s | 6.84 | 7.52 |
| broker (host) | - | - | - | 14.05 | 15.64 |

## Saga step duration p95 (orchestrator metric)

| step | n | p95 s |
|---|---|---|
| CONFIRM_STOCK | 5303 | 0.89 |
| CREATE_ORDER | 5399 | 0.40 |
| MARK_ORDER_PAID | 5322 | 0.42 |
| TAKE_PAYMENT | 5322 | 11.78 |

## Saga end-to-end duration p95

| status | n | p95 s |
|---|---|---|
| COMPLETED | 5303 | 17.72 |

## HTTP handler p95 by service/route (inbound)

| service | route | n | p95 s |
|---|---|---|---|
| inventory-service | GET /internal/v1/reservations | 3.09 | 0.00 |
| inventory-service | POST /internal/v1/reservations | 5401 | 0.02 |
| orchestrator-service | GET /v1/checkouts/{orderID} | 20395 | 0.00 |
| orchestrator-service | POST /v1/checkouts | 5400 | 0.05 |
| payment-service | POST /v1/webhooks/razorpay | 15996 | 0.01 |

## HTTP client call p95 (outbound from service to peer)

| service | peer | n | p95 s |
|---|---|---|---|
| orchestrator-service | inventory-service | 5403 | 0.02 |

## DB statement p95 by service

| service | n | p95 s |
|---|---|---|
| inventory-service | 70663 | 0.00 |
| orchestrator-service | 251720 | 0.00 |
| order-service | 70363 | 0.00 |
| payment-service | 127019 | 0.00 |

## Outbox: row created -> confirmed publish p95

| service | n | p95 s |
|---|---|---|
| inventory-service | 5282 | 0.48 |
| orchestrator-service | 21447 | 0.09 |
| order-service | 10724 | 0.09 |
| payment-service | 5322 | 0.49 |

## Payment gateway call p95

| service | n | p95 s |
|---|---|---|
| payment-service | 5849 | 0.01 |

## Payment webhook processing p95

| service | n | p95 s |
|---|---|---|
| payment-service | 15996 | 0.02 |

## DB transactions in window

| service | name | result | count |
|---|---|---|---|
| orchestrator-service | saga_reply | committed | 21346 |
| inventory-service | create_reservation | committed | 5401 |
| order-service | order_create | committed | 5401 |
| payment-service | payment_initiate | committed | 5401 |
| orchestrator-service | create_saga | committed | 5400 |
| order-service | order_transition | committed | 5322 |
| inventory-service | inventory_CONFIRMED | committed | 5303 |
| orchestrator-service | saga_sweep_step | committed | 36.00 |
