# Next-choke summary — c4-rate-160

measurement 2026-11-09T19:51:50.639572935Z .. +180s; whole run to 2026-11-09T19:56:50.634405132Z

client: offered 28800 accepted 28800 completed 12129 pending 16671; server saga p50/p95/max = 59.4/172.3/183.0 s


## Database pools (measurement window)

| service | cap (min->max) | in-use max | in-use mean | samples within 1 of cap | blocked acquisitions | total wait s | avg wait / blocked |
|---|---|---|---|---|---|---|---|
| orchestrator-service | 20->35 | 35.00 | 20.62 | 57% | 527082 | 11972 | 22.71 ms |
| payment-service | 10->10 | 10.00 | 9.59 | 95% | 222048 | 786 | 3.54 ms |
| inventory-service | 10->10 | 10.00 | 4.38 | 3% | 231 | 1.56 | 6.76 ms |
| order-service | 10->10 | 9.00 | 4.49 | 3% | 0.00 | 0.00 | 0.00 ms |

## Autoscaler gauges (whole run + 60 s)

| metric | min | max | last |
|---|---|---|---|
| webhook workers active | 4.00 | 6.00 | 6.00 |
| ready backlog | 0.00 | 1071 | 0.00 |
| oldest ready age s | 0.00 | 3.18 | 0.00 |
| worker busy ratio | 0.00 | 5.74 | 0.00 |
| orchestrator pool cap | 20.00 | 40.00 | 40.00 |
| pool acquire wait s (avg per blocked) | 0.00 | 0.15 | 0.00 |
| pool scale-up held (guard) | 0.00 | 1.00 | 0.00 |
| db probe latency s | 0.00 | 0.04 | 0.00 |
| db probe baseline s | 0.00 | 0.01 | 0.01 |
| ungranted locks | 0.00 | 2.00 | 0.00 |
| live connections to orchestrator_db | 22.00 | 42.00 | 42.00 |

## Scale events in the run

| series | direction | count |
|---|---|---|
| events | up | 2.05 |
| events | up | 4.10 |

## CPU (container = % of one core; host = % of 2 vCPU)

| service | container mean | container max | host mean | host max | host time >70% |
|---|---|---|---|---|---|
| orchestrator | 90.60 | 119 | 66.93 | 83.92 | 100 s |
| payment | 38.75 | 46.36 | 28.47 | 33.02 | 0 s |
| inventory | 25.63 | 30.47 | 18.01 | 21.14 | 0 s |
| order | 18.74 | 26.55 | 13.77 | 18.02 | 0 s |
| broker host | - | - | 26.93 | 34.52 | 0 s |

## Webhook flow

arrivals/s 294; processed/s 293

## RabbitMQ queues (max >= 5)

| queue | kind | max |
|---|---|---|
| distri.inventory.commands | ready | 1639 |
| distri.orchestrator.replies | ready | 431 |
| distri.order.commands | ready | 575 |
| distri.payment.commands | ready | 169 |
| distri.inventory.commands | unacked | 40.00 |
| distri.orchestrator.replies | unacked | 130 |
| distri.order.commands | unacked | 40.00 |
| distri.payment.commands | unacked | 40.00 |

## Backlogs (max, whole run + 60 s)

| metric | max |
|---|---|
| outbox pending (orchestrator) | 16234 |
| outbox oldest age s (orchestrator) | 60.98 |
| outbox pending (payment) | 613 |
| outbox oldest age s (payment) | 7.15 |
| sagas RUNNING | 16920 |
| saga oldest active age s | 181 |
| reply consumers connected | 24.00 |

## Rates and handler times (measurement window)

| metric | value |
|---|---|
| orchestrator GET polling /s | 1356 |
| orchestrator POST /v1/checkouts p95 s | 0.24 |
| orchestrator DB statements/s | 6695 |
| payment DB statements/s | 2291 |
| inventory handler p95 s | 0.03 |
| order handler p95 s | 0.02 |
| payment-commands handler p95 s | 0.02 |
| orchestrator reply handler p95 s | 0.05 |
| DB statement p95 s, orchestrator | 0.05 |
| DB statement p95 s, payment | 0.01 |
