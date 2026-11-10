# Next-choke summary — c4-rate-100

measurement 2026-11-09T17:06:31.455522055Z .. +180s; whole run to 2026-11-09T17:09:49.61099758Z

client: offered 18000 accepted 18000 completed 18000 pending 0; server saga p50/p95/max = 6.3/11.2/24.2 s


## Database pools (measurement window)

| service | cap (min->max) | in-use max | in-use mean | samples within 1 of cap | blocked acquisitions | total wait s | avg wait / blocked |
|---|---|---|---|---|---|---|---|
| orchestrator-service | 15->15 | 15.00 | 8.86 | 8% | 41157 | 85.60 | 2.08 ms |
| payment-service | 10->10 | 10.00 | 6.84 | 38% | 42521 | 41.27 | 0.97 ms |
| inventory-service | 10->10 | 6.00 | 3.30 | 0% | 31.89 | 0.09 | 2.87 ms |
| order-service | 10->10 | 7.00 | 3.41 | 0% | 0.00 | 0.00 | 0.00 ms |

## Autoscaler gauges (whole run + 60 s)

| metric | min | max | last |
|---|---|---|---|
| webhook workers active | 2.00 | 3.00 | 2.00 |
| ready backlog | 0.00 | 24.00 | 0.00 |
| oldest ready age s | 0.00 | 0.10 | 0.00 |
| worker busy ratio | 0.00 | 2.95 | 0.00 |
| orchestrator pool cap | 15.00 | 15.00 | 15.00 |
| pool acquire wait s (avg per blocked) | 0.00 | 0.01 | 0.00 |
| pool scale-up held (guard) | 0.00 | 0.00 | 0.00 |
| db probe latency s | 0.00 | 0.01 | 0.00 |
| db probe baseline s | 0.00 | 0.00 | 0.00 |
| ungranted locks | 0.00 | 0.00 | 0.00 |
| live connections to orchestrator_db | 16.00 | 18.00 | 16.00 |

## Scale events in the run

| series | direction | count |
|---|---|---|
| events | up | 0.00 |
| events | down | 0.00 |
| events | up | 0.00 |

## CPU (container = % of one core; host = % of 2 vCPU)

| service | container mean | container max | host mean | host max | host time >70% |
|---|---|---|---|---|---|
| orchestrator | 65.88 | 71.84 | 49.49 | 53.94 | 0 s |
| payment | 37.80 | 42.22 | 27.93 | 30.74 | 0 s |
| inventory | 22.11 | 24.28 | 15.84 | 17.10 | 0 s |
| order | 20.49 | 23.15 | 14.70 | 16.32 | 0 s |
| broker host | - | - | 26.57 | 29.32 | 0 s |

## Webhook flow

arrivals/s 298; processed/s 298

## RabbitMQ queues (max >= 5)

| queue | kind | max |
|---|---|---|
| distri.inventory.commands | ready | 23.00 |
| distri.orchestrator.replies | ready | 119 |
| distri.order.commands | ready | 188 |
| distri.inventory.commands | unacked | 20.00 |
| distri.orchestrator.replies | unacked | 70.00 |
| distri.order.commands | unacked | 40.00 |
| distri.payment.commands | unacked | 10.00 |

## Backlogs (max, whole run + 60 s)

| metric | max |
|---|---|
| outbox pending (orchestrator) | 45.00 |
| outbox oldest age s (orchestrator) | 0.09 |
| outbox pending (payment) | 47.00 |
| outbox oldest age s (payment) | 0.44 |
| sagas RUNNING | 893 |
| saga oldest active age s | 28.19 |
| reply consumers connected | 11.00 |

## Rates and handler times (measurement window)

| metric | value |
|---|---|
| orchestrator GET polling /s | 184 |
| orchestrator POST /v1/checkouts p95 s | 0.05 |
| orchestrator DB statements/s | 4155 |
| payment DB statements/s | 2279 |
| inventory handler p95 s | 0.02 |
| order handler p95 s | 0.02 |
| payment-commands handler p95 s | 0.02 |
| orchestrator reply handler p95 s | 0.02 |
| DB statement p95 s, orchestrator | 0.00 |
| DB statement p95 s, payment | 0.00 |
