# Load test 04 — Rate sweep (open loop)

**Result:** PASS as a measurement. A sustainable rate is about **30/s**; a backlog builds from 40/s; the system collapses at 60/s.

## What is tested

How many checkouts per second the stack can absorb when arrivals are fixed (open loop), regardless of whether earlier checkouts have finished. This exposes the difference between *accepted* and *completed* rates.

## How it is run

Case C4: offered rates of 10, 20, 30, 40, 50 and 60 per second; 10 s warm-up, 60 s measured; poll completion; one run per rate. AWS staging stack; simulated gateway with its 5 s pay delay.

## Results

| Offered/s | 10 | 20 | 30 | 40 | 50 | 60 |
|---|---:|---:|---:|---:|---:|---:|
| Accepted per s | 10.0 | 20.0 | 30.0 | 40.0 | 50.0 | 60.0 |
| Completed per s | 10.0 | 20.0 | 30.0 | 40.0 | 50.0 | **19.0** |
| End-to-end p99 (s) | 13.8 | 13.9 | 13.7 | 51.7 | 130.6 | 175.9 |
| Max in flight | 79 | 145 | 261 | 858 | 1190 | 1641 |
| 5xx / transport errors | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 |

- Up to 30/s the system keeps up with flat latency.
- From 40/s a backlog builds: p99 rises from 14 s to 52 s, then 131 s at 50/s, although every offered request is still completed during the run.
- At 60/s: 1,801 checkouts accepted, **571 completed, 1,230 still pending at the drain**. A fast 202 with a growing backlog is not capacity.

## Reading this next to the closed-loop numbers

Closed loop (a fixed number of clients, each waiting for its own checkout) shows about 43/s at 500-700 clients, because clients slow down as latency grows. Open loop (fixed arrivals) shows about 30/s is the rate the stack sustains without a backlog. They measure different things.

## Limits

- One run per rate; poll completion also loads the orchestrator.
- "About 30/s" is read from the jump in p99 between 30/s and 40/s, not from a search for the exact edge.
- Simulated gateway.

**Raw data:** [`sweep-rate-poll.md`](data/limit-sweep/sweep-rate-poll.md), [`.json`](data/limit-sweep/sweep-rate-poll.json)
