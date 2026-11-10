# Next experiment — prepared only. NOTHING BELOW HAS BEEN PROVISIONED OR CHANGED. Waiting for approval.

Candidate (from `02-results-and-decision.md`): **payment-service**, because its webhook inbox is the first stage to fall behind (126.8 processed/s vs 178.2/s arriving at 60/s) while its CPU is ~22%.

## Step 0 — superseded
An earlier draft proposed raising `WEBHOOK_PROCESS_BATCH`. The code and the 0.99 processor utilisation (see `02` section 4b) show batch size is not the limit: one goroutine handles events serially at 7.8 ms each (128.7 events/s = 42.9 checkouts/s). Do not run that variable. The first experiment is Run A vs Run B below (a second payment process; on the same host it needs no new AWS resources, on a second host it does).

## Run A — fixed one: payment-service on 1 instance (the current setup)
## Run B — fixed two: payment-service on 2 instances, same burst
- Same burst in both: open loop 60/s, 60 s warm-up, 180 s measured, 120 s drain, same image `09e4015`, same pools (10/10), same simulator seed and `SIM_PAY_DELAY=5s`, same SKU mix, same command shape as in `02`.
- Payment is consumed from RabbitMQ (competing consumers, no load balancer needed); webhooks are posted by the simulator to a single `RAZORPAY` webhook URL, so the **second instance only receives webhook HTTP traffic if the simulator is pointed at a load balancer or at both**. Inbox *processing* is shared through DB leases (`locked_by`/`locked_until`), which is **untested** across two replicas and is the first thing Run B must verify (duplicate payment effects must stay 0).
- Metrics: the same set as `02` plus per-instance webhook processed/s and per-instance CPU.
- Compare: processed webhooks/s, capture->finish p95, peak backlog, drain time, completed/s, saga p95.

## Run C — EC2 Auto Scaling Group, min 1 / max 2 (only if B shows two machines help)
- Needs **new AWS resources that do not exist in `deploy/cloud/terraform` today**: a launch template (from the existing payment host AMI/user-data), an ASG (min 1, max 2), a scaling policy and alarm, and (for webhook traffic) an ALB + target group + security-group rules. The existing `tools/cloud/cloud-replica.sh` only starts a second *container on the same host*; it is not a second machine and is not used here.
- Measure, from the load-generator clock: T0 scaling trigger (alarm in ALARM) -> T1 instance running -> T2 target healthy -> T3 first webhook processed by the new instance ("useful work"). Then stop traffic, record scale-in time, confirm the ASG returns to 1 and no in-flight attempt is lost, and run the correctness audit: `PHASES=22` via `compose.loadgen.yaml --profile correctness` (`CORRECTNESS_RESET_DATA=false`).
- If the **orchestrator** is chosen instead, use one ALB for both runs: orchestrator receives client HTTP (`POST /v1/checkouts`), so the ALB must front both instances in A and B.

## What needs your approval (stopping here)
| Change | Resource | Rough cost (estimate, check pricing) |
|---|---|---|
| Second payment EC2 (Run B), then terminate | 1 x c7i.large | ~0.1 USD/h in Sydney (estimate) |
| ALB + target group (webhooks / orchestrator) | new | ~0.03 USD/h + LCU (estimate) |
| Launch template + ASG + alarm (Run C) | new | EC2 as above |
| Terraform plan/apply | `deploy/cloud/terraform` | review the plan first |
The six staging instances are currently **running** (~0.5 USD/h combined, estimate).
