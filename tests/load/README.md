# Load tests

## What is being tested

How throughput and latency change as load grows, where they stop scaling, and whether the system stays correct and stable while loaded. Two numbers are always reported separately: **accepted/s** (requests answered with HTTP 202) and **completed/s** (checkouts that reached a final saga state). A fast 202 with a growing backlog is not scaling.

## What has been done

Six load tests on the AWS staging stack on 1 November 2026 (one host per service, one broker host, Neon database, single replicas), driven from a separate load-generator host by the Go load generator in [`tools/loadtesting`](../../tools/loadtesting).

**Every result here uses the simulated gateway.** The simulator was configured with a 5 s pay delay, so about 5 s of each checkout (about 7 s end to end when idle) is a configured delay that no application change removes. These figures are not Razorpay throughput and not real payment latency.

Two measurement styles are used:

- **Closed loop:** a fixed number of clients, each starting a new checkout when its previous one finishes. Throughput is roughly clients ÷ time per checkout.
- **Open loop:** checkouts arrive at a fixed rate whether or not earlier ones finished. This reveals backlog and collapse.

## Verdict

| Result | Value | Peak or sustained |
|---|---|---|
| Idle baseline, 10 clients | 1.55 completed/s, end-to-end p95 11.3 s | sustained (3 × 60 s) |
| Poll-mode peak | 36.9/s at 300 clients, then falling | peak, single run |
| SSE-mode peak | 50.8/s at 1000 clients (tested up to 1000, not a ceiling) | peak, single run |
| 500 clients, SSE | 37.6/s (sweep) and 42.9/s (3-run baseline) | range across runs |
| Open-loop capacity | about 30/s sustainable; backlog from 40/s; collapse at 60/s | single run per rate |
| Soak, 700 clients | 42.45 accepted/s and 42.45 completed/s | sustained over 5 minutes |
| Tuning variables | none of four moved throughput | 3 repetitions each |

## Tests

| # | Test | Verdict |
|---|---|---|
| [01](01-baseline.md) | Idle baseline (10 clients, 3 repetitions) | PASS |
| [02](02-client-sweep-poll.md) | Client sweep, poll completion (1 to 600 clients) | PASS; peak at 300 clients, fall caused by polling load |
| [03](03-client-sweep-sse.md) | Client sweep, SSE completion (500, 1000 clients) | PASS; 50.8/s at 1000 clients |
| [04](04-rate-sweep-open-loop.md) | Rate sweep, open loop (10 to 60 per second) | PASS; ~30/s sustainable |
| [05](05-bottleneck-experiment.md) | Bottleneck experiment (4 variables × 3 runs) | PASS as an experiment; no variable moved throughput |
| [06](06-soak.md) | Soak (700 clients, 5 minutes) | PASS; 5 minutes only |

The overload run (500 clients on a single hot product, which refused 151 requests with 503) is documented as [failure case 11](../failure-cases/11-overload-hot-product.md). The oversell run is a [correctness test](../correctness/oversell.md).

## Limits

- Simulated gateway; not a statement about real payment throughput.
- Single replica of every service; one broker and one database.
- Most sweep points are single runs; repeating the 200-client step gave 25.1 and 31.2/s, so single points carry about 20% noise.
- The soak lasted 5 minutes; slow leaks and database growth are not ruled out.
- The limit behind the ~43/s closed-loop figure at 500-700 clients was not identified.
- Run records (git commit, image tag, Neon branch) were not captured for these runs.

**Raw data:** [`data/`](data/)
