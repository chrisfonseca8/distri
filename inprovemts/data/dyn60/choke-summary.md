# Next-choke summary — c4-rate-060

measurement 2026-11-09T16:48:22.136326048Z .. +180s; whole run to 2026-11-09T16:51:40.047906233Z

client: offered 10801 accepted 10801 completed 10801 pending 0; server saga p50/p95/max = 6.4/10.9/20.9 s


## Database pools (measurement window)

| service | cap (min->max) | in-use max | in-use mean | samples within 1 of cap | blocked acquisitions | total wait s | avg wait / blocked |
|---|---|---|---|---|---|---|---|
| orchestrator-service | 10->10 | 10.00 | 5.24 | 8% | 4239 | 13.08 | 3.09 ms |
| payment-service | 10->10 | 10.00 | 4.41 | 8% | 1131 | 0.86 | 0.76 ms |
| inventory-service | 10->10 | 5.00 | 2.76 | 0% | 9.26 | 0.02 | 2.01 ms |
| order-service | 10->10 | 5.00 | 2.22 | 0% | 0.00 | 0.00 | 0.00 ms |

## Autoscaler gauges (whole run + 60 s)

| metric | min | max | last |
|---|---|---|---|
| webhook workers active | 1.00 | 2.00 | 1.00 |
| ready backlog | 0.00 | 20.00 | 0.00 |
| oldest ready age s | 0.00 | 0.06 | 0.00 |
| worker busy ratio | 0.00 | 1.50 | 0.00 |
| orchestrator pool cap | 10.00 | 10.00 | 10.00 |
| pool acquire wait s (avg per blocked) | 0.00 | 0.03 | 0.00 |
| pool scale-up held (guard) | 0.00 | 1.00 | 0.00 |
| db probe latency s | 0.00 | 0.02 | 0.00 |
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
| orchestrator | 42.13 | 47.73 | 32.45 | 36.12 | 0 s |
| payment | 24.27 | 26.64 | 18.70 | 20.52 | 0 s |
| inventory | 14.78 | 16.43 | 11.36 | 12.46 | 0 s |
| order | 13.58 | 15.36 | 10.47 | 11.80 | 0 s |
| broker host | - | - | 18.90 | 21.58 | 0 s |

## Webhook flow

arrivals/s 178; processed/s 178

## RabbitMQ queues (max >= 5)

| queue | kind | max |
|---|---|---|
| distri.inventory.commands | ready | 14.00 |
| distri.orchestrator.replies | ready | 60.00 |
| distri.order.commands | ready | 25.00 |
| distri.inventory.commands | unacked | 10.00 |
| distri.orchestrator.replies | unacked | 44.00 |
| distri.order.commands | unacked | 13.00 |
| distri.payment.commands | unacked | 7.00 |

## Backlogs (max, whole run + 60 s)

| metric | max |
|---|---|
| outbox pending (orchestrator) | 24.00 |
| outbox oldest age s (orchestrator) | 0.19 |
| outbox pending (payment) | 33.00 |
| outbox oldest age s (payment) | 0.41 |
| sagas RUNNING | 491 |
| saga oldest active age s | 18.98 |
| reply consumers connected | 7.00 |

## Rates and handler times (measurement window)

| metric | value |
|---|---|
| orchestrator GET polling /s | 110 |
| orchestrator POST /v1/checkouts p95 s | 0.05 |
| orchestrator DB statements/s | 2529 |
| payment DB statements/s | 1394 |
| inventory handler p95 s | 0.02 |
| order handler p95 s | 0.02 |
| payment-commands handler p95 s | 0.02 |
| orchestrator reply handler p95 s | 0.02 |
| DB statement p95 s, orchestrator | 0.00 |
| DB statement p95 s, payment | 0.00 |
