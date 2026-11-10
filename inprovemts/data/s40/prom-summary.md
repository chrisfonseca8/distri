# Prometheus timeline summary — c4-rate-040

whole run 2026-11-09T15:11:52.190187962Z .. 2026-11-09T15:15:05.303839522Z; measurement window = +0s .. +180s of the run.


## RabbitMQ queues (max over whole run + 2 min, mean over measurement window)

| queue | kind | max | mean in window |
|---|---|---|---|
| distri.inventory.commands | ready | 5.00 | 0.27 |
| distri.orchestrator.replies | ready | 73.00 | 2.30 |
| distri.order.commands | ready | 4.00 | 0.38 |
| distri.inventory.commands | unacked | 11.00 | 1.16 |
| distri.orchestrator.replies | unacked | 120 | 5.76 |
| distri.order.commands | unacked | 20.00 | 3.27 |
| distri.payment.commands | unacked | 5.00 | 0.65 |

## Consumers connected (autoscale), max

| queue | max |
|---|---|
| distri.inventory.commands | 2.00 |
| distri.orchestrator.replies | 12.00 |
| distri.order.commands | 2.00 |
| distri.payment.commands | 2.00 |

## RabbitMQ consumers (measurement window)

| queue | acked/s | handler p95 s |
|---|---|---|
| distri.inventory.commands | 39.38 | 0.02 |
| distri.orchestrator.replies | 158 | 0.02 |
| distri.order.commands | 79.48 | 0.02 |
| distri.payment.commands | 39.99 | 0.02 |

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
| outbox pending rows | orchestrator-service | 13.00 |
| outbox pending rows | payment-service | 18.00 |
| outbox pending rows | inventory-service | 28.00 |
| outbox pending rows | order-service | 8.00 |
| outbox oldest pending age s | orchestrator-service | 0.08 |
| outbox oldest pending age s | payment-service | 0.48 |
| outbox oldest pending age s | inventory-service | 0.50 |
| outbox oldest pending age s | order-service | 0.07 |
| payment webhook backlog | payment-service | 342 |
| payment webhook oldest pending age s | payment-service | 8.45 |
| payment jobs by status (queued) | payment-service | 10.00 |
| payment jobs (running) | payment-service | 4.00 |
| payment jobs oldest queued age s | payment-service | 0.14 |
| saga oldest active age s | orchestrator-service | 25.27 |
| sagas RUNNING | orchestrator-service | 361 |
| outbox dead rows | orchestrator-service | 0.00 |
| outbox dead rows | payment-service | 0.00 |
| outbox dead rows | inventory-service | 0.00 |
| outbox dead rows | order-service | 0.00 |

## Running sagas by current step (max)

| current_step | max |
|---|---|
| - | 91.00 |
| - | 10.00 |
| - | 29.00 |
| - | 273 |

## CPU in measurement window (container % of one core; host % of 2 vCPU; 'time >70%' = seconds the container exceeded 70% of one core)

| service | container mean | container max | time >70% of a core | host mean | host max |
|---|---|---|---|---|---|
| orchestrator | 33.53 | 37.07 | 0s | 25.77 | 28.96 |
| payment | 17.74 | 19.48 | 0s | 14.34 | 15.92 |
| inventory | 11.14 | 12.33 | 0s | 9.20 | 9.94 |
| order | 9.47 | 10.32 | 0s | 7.82 | 11.98 |
| broker (host) | - | - | - | 16.27 | 18.02 |

## Saga step duration p95 (orchestrator metric)

| step | n | p95 s |
|---|---|---|
| CONFIRM_STOCK | 7091 | 0.90 |
| CREATE_ORDER | 7202 | 0.47 |
| MARK_ORDER_PAID | 7107 | 0.45 |
| TAKE_PAYMENT | 7107 | 15.30 |

## Saga end-to-end duration p95

| status | n | p95 s |
|---|---|---|
| COMPLETED | 7091 | 20.34 |

## HTTP handler p95 by service/route (inbound)

| service | route | n | p95 s |
|---|---|---|---|
| inventory-service | GET /internal/v1/reservations | 3.09 | 0.00 |
| inventory-service | POST /internal/v1/reservations | 7199 | 0.02 |
| orchestrator-service | GET /v1/checkouts/{orderID} | 28673 | 0.01 |
| orchestrator-service | POST /v1/checkouts | 7199 | 0.05 |
| payment-service | POST /v1/webhooks/razorpay | 21359 | 0.01 |

## HTTP client call p95 (outbound from service to peer)

| service | peer | n | p95 s |
|---|---|---|---|
| orchestrator-service | inventory-service | 7203 | 0.02 |

## DB statement p95 by service

| service | n | p95 s |
|---|---|---|
| inventory-service | 94121 | 0.00 |
| orchestrator-service | 334992 | 0.00 |
| order-service | 91090 | 0.00 |
| payment-service | 164398 | 0.00 |

## Outbox: row created -> confirmed publish p95

| service | n | p95 s |
|---|---|---|
| inventory-service | 7069 | 0.49 |
| orchestrator-service | 28607 | 0.10 |
| order-service | 14305 | 0.09 |
| payment-service | 7107 | 0.49 |

## Payment gateway call p95

| service | n | p95 s |
|---|---|---|
| payment-service | 7698 | 0.01 |

## Payment webhook processing p95

| service | n | p95 s |
|---|---|---|
| payment-service | 21340 | 0.02 |

## DB transactions in window

| service | name | result | count |
|---|---|---|---|
| orchestrator-service | saga_reply | committed | 28508 |
| inventory-service | create_reservation | committed | 7199 |
| orchestrator-service | create_saga | committed | 7199 |
| order-service | order_create | committed | 7199 |
| payment-service | payment_initiate | committed | 7198 |
| order-service | order_transition | committed | 7107 |
| inventory-service | inventory_CONFIRMED | committed | 7088 |
| orchestrator-service | saga_sweep_step | committed | 36.00 |
