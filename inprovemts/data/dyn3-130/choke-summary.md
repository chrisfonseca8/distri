# Next-choke summary — c4-rate-130

measurement 2026-11-09T19:43:07.221909135Z .. +180s; whole run to 2026-11-09T19:46:42.576065834Z

client: offered 23401 accepted 23401 completed 23401 pending 0; server saga p50/p95/max = 15.8/26.2/37.9 s


## Database pools (measurement window)

| service | cap (min->max) | in-use max | in-use mean | samples within 1 of cap | blocked acquisitions | total wait s | avg wait / blocked |
|---|---|---|---|---|---|---|---|
| orchestrator-service | 20->20 | 20.00 | 16.19 | 35% | 113901 | 276 | 2.43 ms |
| payment-service | 10->10 | 10.00 | 9.51 | 95% | 254902 | 771 | 3.03 ms |
| inventory-service | 10->10 | 9.00 | 4.14 | 3% | 215 | 0.85 | 3.97 ms |
| order-service | 10->10 | 9.00 | 5.00 | 3% | 0.00 | 0.00 | 0.00 ms |

## Autoscaler gauges (whole run + 60 s)

| metric | min | max | last |
|---|---|---|---|
| webhook workers active | 4.00 | 5.00 | 4.00 |
| ready backlog | 0.00 | 1258 | 0.00 |
| oldest ready age s | 0.00 | 3.74 | 0.00 |
| worker busy ratio | 0.00 | 4.93 | 0.00 |
| orchestrator pool cap | 20.00 | 20.00 | 20.00 |
| pool acquire wait s (avg per blocked) | 0.00 | 0.02 | 0.00 |
| pool scale-up held (guard) | 0.00 | 1.00 | 0.00 |
| db probe latency s | 0.00 | 0.01 | 0.00 |
| db probe baseline s | 0.00 | 0.00 | 0.00 |
| ungranted locks | 0.00 | 1.00 | 0.00 |
| live connections to orchestrator_db | 21.00 | 23.00 | 21.00 |

## Scale events in the run

| series | direction | count |
|---|---|---|
| events | up | 1.02 |
| events | down | 0.00 |
| events | up | 0.00 |

## CPU (container = % of one core; host = % of 2 vCPU)

| service | container mean | container max | host mean | host max | host time >70% |
|---|---|---|---|---|---|
| orchestrator | 82.60 | 91.14 | 60.68 | 66.96 | 0 s |
| payment | 45.34 | 50.79 | 32.98 | 36.86 | 0 s |
| inventory | 26.64 | 30.85 | 18.74 | 21.08 | 0 s |
| order | 21.85 | 24.27 | 15.51 | 17.06 | 0 s |
| broker host | - | - | 30.42 | 34.42 | 0 s |

## Webhook flow

arrivals/s 344; processed/s 344

## RabbitMQ queues (max >= 5)

| queue | kind | max |
|---|---|---|
| distri.inventory.commands | ready | 158 |
| distri.orchestrator.replies | ready | 173 |
| distri.order.commands | ready | 228 |
| distri.payment.commands | ready | 138 |
| distri.inventory.commands | unacked | 40.00 |
| distri.orchestrator.replies | unacked | 120 |
| distri.order.commands | unacked | 40.00 |
| distri.payment.commands | unacked | 20.00 |

## Backlogs (max, whole run + 60 s)

| metric | max |
|---|---|
| outbox pending (orchestrator) | 37.00 |
| outbox oldest age s (orchestrator) | 0.12 |
| outbox pending (payment) | 72.00 |
| outbox oldest age s (payment) | 0.63 |
| sagas RUNNING | 3144 |
| saga oldest active age s | 34.52 |
| reply consumers connected | 13.00 |

## Rates and handler times (measurement window)

| metric | value |
|---|---|
| orchestrator GET polling /s | 429 |
| orchestrator POST /v1/checkouts p95 s | 0.09 |
| orchestrator DB statements/s | 5442 |
| payment DB statements/s | 2624 |
| inventory handler p95 s | 0.02 |
| order handler p95 s | 0.02 |
| payment-commands handler p95 s | 0.02 |
| orchestrator reply handler p95 s | 0.04 |
| DB statement p95 s, orchestrator | 0.01 |
| DB statement p95 s, payment | 0.01 |
