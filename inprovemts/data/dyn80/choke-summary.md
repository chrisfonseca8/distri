# Next-choke summary — c4-rate-080

measurement 2026-11-09T17:00:17.63411453Z .. +180s; whole run to 2026-11-09T17:03:36.087829604Z

client: offered 14400 accepted 14400 completed 14400 pending 0; server saga p50/p95/max = 6.5/11.6/22.1 s


## Database pools (measurement window)

| service | cap (min->max) | in-use max | in-use mean | samples within 1 of cap | blocked acquisitions | total wait s | avg wait / blocked |
|---|---|---|---|---|---|---|---|
| orchestrator-service | 10->20 | 20.00 | 8.57 | 27% | 40620 | 1340 | 32.99 ms |
| payment-service | 10->10 | 10.00 | 6.95 | 35% | 22391 | 41.79 | 1.87 ms |
| inventory-service | 10->10 | 8.00 | 3.03 | 0% | 291 | 3.04 | 10.45 ms |
| order-service | 10->10 | 8.00 | 2.49 | 0% | 0.00 | 0.00 | 0.00 ms |

## Autoscaler gauges (whole run + 60 s)

| metric | min | max | last |
|---|---|---|---|
| webhook workers active | 1.00 | 3.00 | 1.00 |
| ready backlog | 0.00 | 21.00 | 0.00 |
| oldest ready age s | 0.00 | 0.72 | 0.00 |
| worker busy ratio | 0.00 | 2.64 | 0.00 |
| orchestrator pool cap | 10.00 | 20.00 | 20.00 |
| pool acquire wait s (avg per blocked) | 0.00 | 0.29 | 0.00 |
| pool scale-up held (guard) | 0.00 | 1.00 | 0.00 |
| db probe latency s | 0.00 | 0.01 | 0.00 |
| db probe baseline s | 0.00 | 0.00 | 0.00 |
| ungranted locks | 0.00 | 0.00 | 0.00 |
| live connections to orchestrator_db | 12.00 | 22.00 | 21.00 |

## Scale events in the run

| series | direction | count |
|---|---|---|
| events | up | 0.00 |
| events | up | 1.01 |

## CPU (container = % of one core; host = % of 2 vCPU)

| service | container mean | container max | host mean | host max | host time >70% |
|---|---|---|---|---|---|
| orchestrator | 55.12 | 61.99 | 41.74 | 46.84 | 0 s |
| payment | 30.71 | 34.32 | 23.04 | 25.34 | 0 s |
| inventory | 18.18 | 20.30 | 13.67 | 15.00 | 0 s |
| order | 17.16 | 19.38 | 12.78 | 14.42 | 0 s |
| broker host | - | - | 23.22 | 25.72 | 0 s |

## Webhook flow

arrivals/s 237; processed/s 237

## RabbitMQ queues (max >= 5)

| queue | kind | max |
|---|---|---|
| distri.inventory.commands | ready | 16.00 |
| distri.orchestrator.replies | ready | 163 |
| distri.order.commands | ready | 127 |
| distri.inventory.commands | unacked | 24.00 |
| distri.orchestrator.replies | unacked | 210 |
| distri.order.commands | unacked | 30.00 |
| distri.payment.commands | unacked | 20.00 |

## Backlogs (max, whole run + 60 s)

| metric | max |
|---|---|
| outbox pending (orchestrator) | 65.00 |
| outbox oldest age s (orchestrator) | 0.16 |
| outbox pending (payment) | 55.00 |
| outbox oldest age s (payment) | 1.00 |
| sagas RUNNING | 914 |
| saga oldest active age s | 21.15 |
| reply consumers connected | 21.00 |

## Rates and handler times (measurement window)

| metric | value |
|---|---|
| orchestrator GET polling /s | 149 |
| orchestrator POST /v1/checkouts p95 s | 0.14 |
| orchestrator DB statements/s | 3321 |
| payment DB statements/s | 1846 |
| inventory handler p95 s | 0.02 |
| order handler p95 s | 0.02 |
| payment-commands handler p95 s | 0.02 |
| orchestrator reply handler p95 s | 0.04 |
| DB statement p95 s, orchestrator | 0.00 |
| DB statement p95 s, payment | 0.00 |
