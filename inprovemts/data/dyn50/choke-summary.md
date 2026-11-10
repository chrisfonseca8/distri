# Next-choke summary — c4-rate-050

measurement 2026-11-09T16:41:53.339445113Z .. +180s; whole run to 2026-11-09T16:45:12.89447939Z

client: offered 9000 accepted 9000 completed 9000 pending 0; server saga p50/p95/max = 6.2/10.8/22.4 s


## Database pools (measurement window)

| service | cap (min->max) | in-use max | in-use mean | samples within 1 of cap | blocked acquisitions | total wait s | avg wait / blocked |
|---|---|---|---|---|---|---|---|
| orchestrator-service | 10->10 | 10.00 | 4.19 | 8% | 5405 | 15.09 | 2.79 ms |
| payment-service | 10->10 | 7.00 | 3.51 | 0% | 667 | 0.54 | 0.82 ms |
| inventory-service | 10->10 | 5.00 | 2.43 | 0% | 0.00 | 0.00 | 0.00 ms |
| order-service | 10->10 | 8.00 | 1.78 | 0% | 0.00 | 0.00 | 0.00 ms |

## Autoscaler gauges (whole run + 60 s)

| metric | min | max | last |
|---|---|---|---|
| webhook workers active | 1.00 | 2.00 | 1.00 |
| ready backlog | 0.00 | 27.00 | 0.00 |
| oldest ready age s | 0.00 | 0.10 | 0.00 |
| worker busy ratio | 0.00 | 1.43 | 0.00 |
| orchestrator pool cap | 10.00 | 10.00 | 10.00 |
| pool acquire wait s (avg per blocked) | 0.00 | 0.01 | 0.00 |
| pool scale-up held (guard) | 0.00 | 0.00 | 0.00 |
| db probe latency s | 0.00 | 0.01 | 0.00 |
| db probe baseline s | 0.00 | 0.00 | 0.00 |
| ungranted locks | 0.00 | 0.00 | 0.00 |
| live connections to orchestrator_db | 11.00 | 12.00 | 11.00 |

## Scale events in the run

| series | direction | count |
|---|---|---|
| events | up | 0.00 |
| events | down | 0.00 |

## CPU (container = % of one core; host = % of 2 vCPU)

| service | container mean | container max | host mean | host max | host time >70% |
|---|---|---|---|---|---|
| orchestrator | 35.83 | 40.19 | 27.89 | 30.84 | 0 s |
| payment | 20.95 | 23.08 | 16.38 | 18.06 | 0 s |
| inventory | 13.62 | 15.33 | 10.68 | 11.84 | 0 s |
| order | 11.49 | 12.95 | 9.21 | 10.14 | 0 s |
| broker host | - | - | 17.16 | 19.24 | 0 s |

## Webhook flow

arrivals/s 148; processed/s 148

## RabbitMQ queues (max >= 5)

| queue | kind | max |
|---|---|---|
| distri.inventory.commands | ready | 14.00 |
| distri.orchestrator.replies | ready | 13.00 |
| distri.order.commands | ready | 41.00 |
| distri.inventory.commands | unacked | 14.00 |
| distri.orchestrator.replies | unacked | 34.00 |
| distri.order.commands | unacked | 13.00 |
| distri.payment.commands | unacked | 6.00 |

## Backlogs (max, whole run + 60 s)

| metric | max |
|---|---|
| outbox pending (orchestrator) | 26.00 |
| outbox oldest age s (orchestrator) | 0.09 |
| outbox pending (payment) | 22.00 |
| outbox oldest age s (payment) | 0.49 |
| sagas RUNNING | 367 |
| saga oldest active age s | 21.13 |
| reply consumers connected | 8.00 |

## Rates and handler times (measurement window)

| metric | value |
|---|---|
| orchestrator GET polling /s | 88.75 |
| orchestrator POST /v1/checkouts p95 s | 0.05 |
| orchestrator DB statements/s | 2096 |
| payment DB statements/s | 1171 |
| inventory handler p95 s | 0.02 |
| order handler p95 s | 0.02 |
| payment-commands handler p95 s | 0.02 |
| orchestrator reply handler p95 s | 0.02 |
| DB statement p95 s, orchestrator | 0.00 |
| DB statement p95 s, payment | 0.00 |
