# Final record: 130/s and 160/s results, and a 200/s estimate (2026-11-11)

Status: **reported** (numbers come from the runner's own `load-test.md`/`.json` in `inprovemts/data/<run>/`, the orchestrator and payment scaler logs, and SQL read from the staging databases). The Neon CPU figures are read off a console screenshot and are approximate. The 200/s section is an **estimate, not a measurement**.

All runs use the **simulated gateway** (Razorpay simulator, success mode). Nothing here is real-Razorpay or real-payment throughput. Every figure is a single run unless stated; none is a repeated measurement.

## 1. What was measured

Workload: open-loop `C4`, uniform over 50 products, 60 s warm-up + 180 s measured, status polled every 5 s, simulated pay delay about 5 s.
"Completed" = checkout reached a terminal order state inside the run window. "Accepted" = HTTP 202. The two are reported separately on purpose: a fast 202 with a growing backlog is acceptance, not completion.

### 1.1 Valid comparisons

| | Offered | Accepted | Completed in window | Saga p50 / p95 / max | Orchestrator CPU (of one core) mean / max | Verdict |
|---|---:|---:|---:|---|---|---|
| **130/s**, first project, orchestrator on its own host (payment pool trigger 5 ms; payment pool never resized) | 23,401 | 23,401 | 23,401 | 15.8 / 26.2 / 37.9 s | 83 / 91% | passed, slow |
| **130/s**, `asg130` (ASG instance behind the ALB, payment trigger 2 ms, orchestrator pool cap 20) | 23,401 | 23,401 | **23,401** | **6.5 / 11.1 / 23.7 s** | 87 / 126% | **PASS**, 10/10 invariants |
| **160/s**, first project (as row 1) | 28,800 | 28,800 | 12,129 (16,671 pending at the end of the drain) | 59.4 / 172.3 / 183.0 s | 91 / 119% | overload |
| **160/s**, `asg160` (as row 2) | 28,800 | 28,800 | **16,221** (90.1/s; 12,579 pending) | 66.3 / 144.9 / 152.9 s | 105 / 156% | overload, 10/10 invariants |

130/s detail (`asg130` against the first-project run):
- Payment DB pool: blocked acquisitions went from 254,902 to 23,274 and total wait from 771 s to 17.3 s. The pool resized 10 to 15 once the trigger was lowered from 5 ms to 2 ms. The old trigger needed an average wait of at least 5 ms per blocked call and was blind to many short waits.
- Checkout p50 / p95: 38.5 / 67.1 ms. Maximum in-flight workflows: 1,382.

160/s detail (`asg160`):
- Accepted 160.00/s with zero 5xx; completed only 90.1/s. In-flight workflows reached 14,097.
- Orchestrator DB pool was pinned at its cap of 20 (817,199 waits, 2 h 0 m of total wait). The payment pool resized once (10 to 15).

### 1.2 Runs that are NOT valid capacity results

| Run | What went wrong | Use it for |
|---|---|---|
| `dyn130` (10-minute window) | Neon project hit its 1,024 MB size limit; services failed writes, 28,178 messages were dead-lettered, sagas went STUCK | the storage lesson only |
| `failed1-asg130/160-no-telemetry` | runner could not find the orchestrator container (tunnel pointed at a stopped host) | nothing |
| `r130-failed-pool50` (pool 50, new Neon project) | 6,770 of 23,401 completed in window (37.6/s), saga p95 213 s | showing the pool cap was not the cause |
| `r130` (pool 20, new Neon project) | 5,252 of 23,401 completed in window (29.2/s), saga p95 225 s | showing the new database was the limit |

Both `r130` runs were database-limited: the Neon console shows the new project's compute at about 1.6 to 1.8 vCPU of its 2 vCPU autoscaling ceiling during each run (screenshot, approximate; the compute idles at 0.5 vCPU and has to scale up first). Checkout p50 was about 2x worse from the first minute, orchestrator DB pool waits reached 8 h (against 7 min in the passing run), and the orchestrator served about 485,000 requests (against 67,731) because status polls multiply while sagas are slow.

The one invariant failure reported for `r130` ("paid reservations are confirmed", 4,181 violations) was a **timing artifact**: the runner checked while the backlog was still draining, and by design an order is marked PAID before the inventory confirm lands. Re-checked after the drain: 23,401 of 23,401 orders PAID, 0 violations on that check, 0 paid orders without a successful payment, 0 duplicate payments, 0 stock-bound violations. Dead letters did not grow (28,178 before and after).

## 2. What the data supports

1. **The accept path is never the limit.** Accepted equals offered at 130 and 160/s in every run, with zero 5xx; checkout p95 was 67 ms in the healthy run. Completion is what saturates.
2. **On one orchestrator instance, completion holds up to about 130/s and breaks before 160/s.** At 130/s the orchestrator container used 87% of a core on average and peaked at 126%; at 160/s it was pinned at 105% mean and 156% max and its pool sat at the cap.
3. **The next limit after the orchestrator is the database.** On the free Neon compute (2 vCPU ceiling, one compute for all four databases) 130/s offered saturated the CPU and completion fell to 29 to 38/s.
4. **Slowness feeds on itself.** Every in-flight workflow is polled every 5 s, so slow completion raises read load on the orchestrator and the database, which slows completion further. This is why a small difference (a different database project) turned a pass into a 4x collapse.
5. **The payment pool autoscaler had a real blind spot, now fixed.** A trigger of 5 ms average wait ignored thousands of short waits; at 2 ms it fires and cuts total payment pool wait about 45x at 130/s. The trigger is still an average per blocked call, so a total-wait trigger would be sturdier.
6. **EC2 autoscaling on 50% CPU works, but not inside a 4-minute window.** The alarm needs three consecutive minutes above 50%. Measured from the runner start: `dyn130` raised the desired size at +4.0 min and had the new instance healthy at +7.1 min; `r130` at +5.9 min and +9.7 min. The delay after the request is about 1 minute to boot plus about 2.5 minutes for the ALB's 5 healthy checks at 30 s. A benchmark shorter than 7 to 10 minutes sees one instance.
7. **Storage is a hard cap on test length.** Measured 14 to 16 KB per workflow across the four databases (19.4 MB for 1,200 workflows; about 336 MB for 23,401). The project cap of 1,024 MB is about 65,000 workflows, which is about 8 minutes at 130/s. The runner truncates all service tables at the start of every run, so only one run's data is held at a time.
8. **Correctness held under overload.** No oversell, no duplicate payments, stock within bounds, and no new dead letters in the valid or the database-limited runs. Overload produced delay, never wrong state.

## 3. Estimate for 200/s (not measured)

Method: scale the measured healthy 130/s run (`asg130`) by 200/130 = 1.54, then compare with the measured overload at 160/s. Confidence is medium for the application CPU figures and low for the database.

| Resource | Measured at 130/s | Estimated at 200/s | Meaning |
|---|---|---|---|
| Orchestrator CPU | 87% mean, 126% max of a core | about 134% mean, about 195% max | needs at least 2 instances; 1 cannot hold it |
| Payment CPU | 67% mean, 92% max | about 102% mean, about 142% max | borderline for one host; a second replica would be prudent |
| Inventory CPU | 21% mean, 34% max | about 33% mean, about 53% max | fine |
| In-flight workflows (Little's law, same latency) | 1,239 | about 1,900 | |
| Status polls per second (in-flight x 0.2) | about 250 | about 380 | more read load on the orchestrator and DB |
| Storage per 4-minute run (60 s warm-up + 180 s measured) | 23,401 workflows, about 0.34 GB | 48,000 workflows, about 0.7 to 0.8 GB | fits the 1 GB cap with little margin; no 10-minute runs |
| Postgres CPU | saturated 2 vCPU in the database-limited runs | unknown; at least above 2 vCPU | needs a compute larger than the free plan's 2 vCPU, or the databases split across computes |

**Scenario A: today's deployment (1 orchestrator, 1 payment, one free Neon compute): it will overload.**
- Taking the measured 160/s completion ceiling (about 90/s) as given, a 180 s window at 200/s offers 36,000 and completes about 16,000, leaving about 20,000 pending (12,579 at 160/s).
- Saga completion p95 would be around 3 to 4 minutes, against 145 s at 160/s. Peak in-flight would be about 20,000, against 14,097.
- On the free Neon compute that failed at 130/s, the ceiling would be lower still (about 30 to 40/s), and the backlog larger.
- Acceptance would stay at about 200/s with no 5xx, because the accept path is cheap. That is exactly the "fast 202 with a growing backlog" pattern.

**Scenario B: provisioned for it (could reach 200/s; cannot be confirmed without a run):**
- at least 2 orchestrator instances running before the load starts (the CPU policy cannot get a second instance serving inside a short run);
- a Postgres compute with more than 2 vCPU, or the orchestrator database on a separate compute from order/inventory/payment;
- a second payment replica;
- a lower polling rate in the load generator (for example 10 s) or completion over SSE;
- at that point the expected latency is the healthy one (saga p50 about 6.5 s, p95 about 11 to 15 s, set mostly by the simulated pay delay), with about 67% of a core on each of 2 orchestrator instances.

I would not quote 200/s as achievable on this stack. The estimate says the application tier can reach it with two instances; the database tier is the unknown, and that is where it most likely fails.

### 3.1 Estimate for 160/s with two orchestrator instances from the start (NOT measured)
The measured 160/s result (`asg160`, section 1.1) used one orchestrator instance and is unchanged. This is a what-if for the same settings (payment trigger 2 ms, orchestrator pool cap 20 per instance, ALB in front) with **two** orchestrator instances serving for the whole window, on a database with the headroom the first project showed at 160/s (DB probe latency flat at 4 to 9 ms). It does not apply to the current free Neon compute, which failed at 130/s.

Basis: at 160/s the single instance was pinned at 105% mean and 156% max of a core with its pool at the cap, while the database stayed flat; the healthy 130/s run had the same instance at 87% CPU and passed with saga p95 11 s. Splitting 160/s over two instances puts each at about 53% of a core mean (about 78% max), below the load where one instance passed, and doubles total DB concurrency to 40 connections.

| | Measured, 1 instance (`asg160`) | Estimated, 2 instances |
|---|---|---|
| Completed in the 180 s window | 16,221 of 28,800 (56%) | about 90 to 100% of 28,800 |
| Pending when the drain ends | 12,579 | roughly 0 to 3,000 |
| Saga p50 / p95 | 66 / 145 s | about 7 to 9 s / 12 to 30 s |
| Peak in-flight workflows | 14,097 | about 1,800 to 3,000 |
| Orchestrator CPU per instance (of one core) | 105% mean, 156% max | about 53% mean, about 78% max |

Confidence is low to medium. It assumes load splits evenly through the ALB, that payment (58% mean, 94% max at 160/s) does not become the limit, and that the database does not saturate. In practice a second instance needs about 7 to 10 minutes to start serving, so it must be started before the run, not by the CPU policy. A run is the only way to confirm it.

## 4. Limits of this work
- Single runs, not repeated; the 130/s knee is sharp enough that a repeat could flip a pass to a fail.
- The passing and failing 130/s runs differ in the database project (and so in its compute), so that comparison is indicative, not controlled. The old project could not be inspected afterwards.
- The 200/s numbers assume linear scaling, which the polling feedback loop makes optimistic near the knee.
- Neon CPU values come from one console screenshot.
- Storage per workflow was measured on small and mid-sized runs and may differ at scale.

## 5. State left behind (project stopped 2026-11-11)
Stopped, not deleted: broker, inventory, payment, order, load-generator, the original orchestrator host, and the extra test host `i-098e8e80625ae08a2` (its disk can be deleted if unwanted). The ASG `distri-staging-orch-asg` is at min 0 / desired 0 / max 2, still on launch template v7; the CPU policy `cpu50-scale-out` (50%, scale-in disabled) is left attached. Launch template versions 1 to 7 remain.

Still costing money while everything is stopped: the internal ALB `distri-staging-orch-alb` (and its target group), and 9 EBS volumes (about 246 GiB: broker 20 + 50 GiB RabbitMQ data volume, 30 each on four service hosts, 40 load-generator, 8 on the test host and the terminated ASG instance's volume). No Elastic IPs and no NAT gateway.

Staging data: the Neon project `<new-neon-project>` holds the last run's data (about 391 MB of 1,024 MB). The next run's reset clears it. RabbitMQ holds 28,178 dead-lettered messages from the failed storage incident in `distri.commands.dead`.

Host configuration (each has a `.bak-*` copy): payment `.env.payment` sets `PAYMENT_DB_POOL_UP_WAIT=2ms`; all service env files point at the new Neon project; the load-generator's tunnel and `ORCHESTRATOR_URL` are back on the original orchestrator host (10.40.1.10:8083) while its database settings point at the new project; the original orchestrator's container is stopped and must be started (`docker start`) after the hosts come back.

Restart checklist: start broker, inventory, payment, order, then the orchestrator host and its container, then the load-generator; confirm the `.env` database URLs, one consumer per queue and an empty dead-letter baseline before measuring; record the Neon size before and after every run.

Rotate: the Neon admin password and the service role passwords were pasted into a chat during this work, and the old project's service password too. Re-run `provision-neon.sh` for new service passwords and rotate the admin password in the Neon console, then update the SSM parameter `/distri/staging/orchestrator/database-url`.

## 6. Deleted to stop billing (2026-11-11) and how to recreate it
Deleted: the internal ALB `distri-staging-orch-alb` and the scaling policy `cpu50-scale-out` (its CloudWatch alarms go with it). Kept (free): the target group `distri-staging-orch-tg`, the ALB security group `distri-staging-alb`, the ASG definition (min 0 / desired 0 / max 2, template v7) and the launch template versions.

- **ALB:** internal, application, IPv4, VPC `vpc-06a358a98c4efa863`, subnets `subnet-0645de85cf8380a96` (ap-southeast-2a) and `subnet-0e602ce7fd3b6ad87` (ap-southeast-2b), security group `sg-0b3c152109b30ce1a` (`distri-staging-alb`; inbound TCP 80 from the load-generator's security group `sg-056a8fc47aad1313e`), idle timeout 60 s. One listener: HTTP 80 forwarding to the target group.
- **Target group:** HTTP 8083, target type instance, health check `GET /readyz` expecting 200, interval 30 s, timeout 5 s, healthy threshold 5, unhealthy threshold 2, deregistration delay 300 s.
- **Scaling policy:** target tracking on `ASGAverageCPUUtilization`, target 50, scale-in disabled, estimated instance warm-up 180 s. For a faster response use a lower healthy threshold on the target group and an alarm with one datapoint (step scaling).
- After recreating the ALB, the load-generator's `ORCHESTRATOR_URL` can point at the new ALB DNS name (`http://<dns>`), and the ASG needs the target group attached again (it keeps the attachment).
