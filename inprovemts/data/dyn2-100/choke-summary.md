# Next-choke summary — c4-rate-100

measurement 2026-11-09T19:26:14.887890882Z .. +180s; whole run to 2026-11-09T19:29:33.977645589Z

client: offered 18000 accepted 18000 completed 18000 pending 0; server saga p50/p95/max = 6.2/10.9/24.0 s


## Database pools (measurement window)

| service | cap (min->max) | in-use max | in-use mean | samples within 1 of cap | blocked acquisitions | total wait s | avg wait / blocked |
|---|---|---|---|---|---|---|---|
| orchestrator-service | 15->15 | 15.00 | 8.76 | 19% | 66219 | 198 | 2.99 ms |
| payment-service | 10->10 | 10.00 | 6.35 | 32% | 54147 | 60.86 | 1.12 ms |
| inventory-service | 10->10 | 7.00 | 3.22 | 0% | 99.77 | 0.43 | 4.30 ms |
| order-service | 10->10 | 7.00 | 3.51 | 0% | 0.00 | 0.00 | 0.00 ms |

## Autoscaler gauges (whole run + 60 s)

| metric | min | max | last |
|---|---|---|---|
| webhook workers active | 3.00 | 4.00 | 3.00 |
| ready backlog | 0.00 | 19.00 | 0.00 |
| oldest ready age s | 0.00 | 0.05 | 0.00 |
| worker busy ratio | 0.00 | 2.81 | 0.00 |
| orchestrator pool cap | 15.00 | 15.00 | 15.00 |
| pool acquire wait s (avg per blocked) | 0.00 | 0.01 | 0.00 |
| pool scale-up held (guard) | 0.00 | 1.00 | 0.00 |
| db probe latency s | 0.00 | 0.01 | 0.00 |
| db probe baseline s | 0.00 | 0.00 | 0.00 |
| ungranted locks | 0.00 | 1.00 | 0.00 |
| live connections to orchestrator_db | 16.00 | 17.00 | 16.00 |

## Scale events in the run

| series | direction | count |
|---|---|---|
| events | up | 0.00 |
| events | down | 0.00 |
| events | up | 0.00 |

## CPU (container = % of one core; host = % of 2 vCPU)

| service | container mean | container max | host mean | host max | host time >70% |
|---|---|---|---|---|---|
| orchestrator | 62.98 | 72.45 | 47.58 | 53.58 | 0 s |
| payment | 35.69 | 40.76 | 27.07 | 30.14 | 0 s |
| inventory | 22.07 | 25.04 | 15.86 | 17.70 | 0 s |
| order | 17.78 | 20.09 | 13.18 | 14.58 | 0 s |
| broker host | - | - | 26.43 | 29.76 | 0 s |

## Webhook flow

arrivals/s 292; processed/s 292

## RabbitMQ queues (max >= 5)

| queue | kind | max |
|---|---|---|
| distri.inventory.commands | ready | 132 |
| distri.order.commands | ready | 88.00 |
| distri.payment.commands | ready | 24.00 |
| distri.inventory.commands | unacked | 20.00 |
| distri.orchestrator.replies | unacked | 22.00 |
| distri.order.commands | unacked | 30.00 |
| distri.payment.commands | unacked | 20.00 |

## Backlogs (max, whole run + 60 s)

| metric | max |
|---|---|
| outbox pending (orchestrator) | 50.00 |
| outbox oldest age s (orchestrator) | 0.10 |
| outbox pending (payment) | 42.00 |
| outbox oldest age s (payment) | 0.49 |
| sagas RUNNING | 705 |
| saga oldest active age s | 24.43 |
| reply consumers connected | 17.00 |

## Rates and handler times (measurement window)

| metric | value |
|---|---|
| orchestrator GET polling /s | 176 |
| orchestrator POST /v1/checkouts p95 s | 0.05 |
| orchestrator DB statements/s | 4091 |
| payment DB statements/s | 2301 |
| inventory handler p95 s | 0.02 |
| order handler p95 s | 0.02 |
| payment-commands handler p95 s | 0.02 |
| orchestrator reply handler p95 s | 0.02 |
| DB statement p95 s, orchestrator | 0.00 |
| DB statement p95 s, payment | 0.00 |
