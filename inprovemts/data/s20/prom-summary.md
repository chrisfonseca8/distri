# Prometheus timeline summary — c4-rate-020

whole run 2026-11-09T15:01:11.429520405Z .. 2026-11-09T15:04:24.152955962Z; measurement window = +0s .. +180s of the run.


## RabbitMQ queues (max over whole run + 2 min, mean over measurement window)

| queue | kind | max | mean in window |
|---|---|---|---|
| distri.inventory.commands | ready | 23.00 | 0.00 |
| distri.orchestrator.replies | ready | 50.00 | 2.54 |
| distri.order.commands | ready | 28.00 | 1.51 |
| distri.inventory.commands | unacked | 10.00 | 0.97 |
| distri.orchestrator.replies | unacked | 60.00 | 3.92 |
| distri.order.commands | unacked | 10.00 | 1.65 |
| distri.payment.commands | unacked | 2.00 | 0.27 |

## Consumers connected (autoscale), max

| queue | max |
|---|---|
| distri.inventory.commands | 3.00 |
| distri.orchestrator.replies | 6.00 |
| distri.order.commands | 3.00 |
| distri.payment.commands | 2.00 |

## RabbitMQ consumers (measurement window)

| queue | acked/s | handler p95 s |
|---|---|---|
| distri.inventory.commands | 19.72 | 0.02 |
| distri.orchestrator.replies | 79.36 | 0.02 |
| distri.order.commands | 39.81 | 0.02 |
| distri.payment.commands | 20.01 | 0.02 |

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
| outbox pending rows | orchestrator-service | 158 |
| outbox pending rows | payment-service | 15.00 |
| outbox pending rows | inventory-service | 18.00 |
| outbox pending rows | order-service | 6.00 |
| outbox oldest pending age s | orchestrator-service | 6.32 |
| outbox oldest pending age s | payment-service | 0.46 |
| outbox oldest pending age s | inventory-service | 2.86 |
| outbox oldest pending age s | order-service | 0.07 |
| payment webhook backlog | payment-service | 277 |
| payment webhook oldest pending age s | payment-service | 9.13 |
| payment jobs by status (queued) | payment-service | 6.00 |
| payment jobs (running) | payment-service | 3.00 |
| payment jobs oldest queued age s | payment-service | 0.16 |
| saga oldest active age s | orchestrator-service | 20.69 |
| sagas RUNNING | orchestrator-service | 260 |
| outbox dead rows | orchestrator-service | 0.00 |
| outbox dead rows | payment-service | 0.00 |
| outbox dead rows | inventory-service | 0.00 |
| outbox dead rows | order-service | 0.00 |

## Running sagas by current step (max)

| current_step | max |
|---|---|
| - | 19.00 |
| - | 178 |
| - | 15.00 |
| - | 240 |

## CPU in measurement window (container % of one core; host % of 2 vCPU; 'time >70%' = seconds the container exceeded 70% of one core)

| service | container mean | container max | time >70% of a core | host mean | host max |
|---|---|---|---|---|---|
| orchestrator | 18.64 | 24.20 | 0s | 15.41 | 19.40 |
| payment | 10.41 | 13.92 | 0s | 9.23 | 12.06 |
| inventory | 6.63 | 8.48 | 0s | 6.31 | 7.42 |
| order | 5.83 | 7.11 | 0s | 5.58 | 6.68 |
| broker (host) | - | - | - | 12.25 | 14.58 |

## Saga step duration p95 (orchestrator metric)

| step | n | p95 s |
|---|---|---|
| CONFIRM_STOCK | 3559 | 0.90 |
| CREATE_ORDER | 3600 | 2.19 |
| MARK_ORDER_PAID | 3559 | 0.28 |
| TAKE_PAYMENT | 3567 | 19.71 |

## Saga end-to-end duration p95

| status | n | p95 s |
|---|---|---|
| COMPLETED | 3559 | 24.01 |

## HTTP handler p95 by service/route (inbound)

| service | route | n | p95 s |
|---|---|---|---|
| inventory-service | GET /internal/v1/reservations | 3.09 | 0.00 |
| inventory-service | POST /internal/v1/reservations | 3601 | 0.02 |
| orchestrator-service | GET /v1/checkouts/{orderID} | 14461 | 0.02 |
| orchestrator-service | POST /v1/checkouts | 3600 | 1.06 |
| payment-service | POST /v1/webhooks/razorpay | 10711 | 0.01 |

## HTTP client call p95 (outbound from service to peer)

| service | peer | n | p95 s |
|---|---|---|---|
| orchestrator-service | inventory-service | 3603 | 0.02 |

## DB statement p95 by service

| service | n | p95 s |
|---|---|---|
| inventory-service | 47337 | 0.00 |
| orchestrator-service | 172180 | 0.00 |
| order-service | 49098 | 0.00 |
| payment-service | 88093 | 0.00 |

## Outbox: row created -> confirmed publish p95

| service | n | p95 s |
|---|---|---|
| inventory-service | 3550 | 0.49 |
| orchestrator-service | 14319 | 0.10 |
| order-service | 7159 | 0.09 |
| payment-service | 3561 | 0.49 |

## Payment gateway call p95

| service | n | p95 s |
|---|---|---|
| payment-service | 4091 | 0.01 |

## Payment webhook processing p95

| service | n | p95 s |
|---|---|---|
| payment-service | 10706 | 0.02 |

## DB transactions in window

| service | name | result | count |
|---|---|---|---|
| orchestrator-service | saga_reply | committed | 14285 |
| inventory-service | create_reservation | committed | 3601 |
| payment-service | payment_initiate | committed | 3601 |
| orchestrator-service | create_saga | committed | 3600 |
| order-service | order_create | committed | 3600 |
| order-service | order_transition | committed | 3565 |
| inventory-service | inventory_CONFIRMED | committed | 3550 |
| orchestrator-service | saga_sweep_step | committed | 36.00 |
