### Findings (each is backed by a run in this report)

1. **`kill -9` of payment-service during a gateway call leaves payments ambiguous, and nothing resolves them.**
   - Two independent chaos runs (`payment-kill9-mid-flight`): 4 of 120 sagas stayed `RUNNING` with the payment `UNKNOWN`, order `PENDING_PAYMENT`, stock held. No duplicate charge, no invariant broken — this is the designed behaviour (an ambiguous call is never blindly retried).
   - The graceful path (`payment-sigterm-graceful`, SIGTERM with a 30 s grace) left **0** ambiguous payments in two runs: the in-flight jobs drained first.
   - Before the load runs the local database held 37 sagas `STUCK` at `TAKE_PAYMENT` and 8 `RUNNING` (saved in `scratchpad/pre-load-stuck-summary.txt` and the pre-load dump). All 37 `STUCK` ones are stuck at `TAKE_PAYMENT`, which matches this pattern; that they came from earlier crash testing is an inference, not something I verified. The stuck-refund rule only applies to sagas stuck *after* payment, so these are left for an operator (`admin revive`) and keep their stock reserved until then; I did not observe how the `RUNNING` ones end because the load runner truncates the tables.
   - Consequence: rolling restarts must use SIGTERM and the compose `stop_grace_period`; `kill -9` costs money-state ambiguity, not just a retry.

2. **No duplicate effects under any fault.** Across 10 scenarios (orchestrator, order-service, inventory-service, payment-service killed with `-9`; payment-service stopped gracefully; RabbitMQ restarted; three retry chains; two live orchestrator replicas): zero extra payments, zero extra reservations, no accepted saga without its order, all eight database invariants unchanged, dead-letter queue unchanged. The harness only counts a scenario if at least one saga was in flight when the fault landed (36–48 were).

3. **Retry chain verified on the three queues that had never been exercised** (payment commands, inventory commands, orchestrator replies): each held the failing message in the 5 s tier, then the 30 s tier, and the saga completed once the cause was fixed. Previously only the order queue was proven (EXP-12).

4. **Two orchestrator replicas share the work safely**: 80/80 sagas completed, no duplicate effects, both replicas active. The test-level proof is `TestTwoSweepersNeverResendTheSameStepTwice` (fails when the row lock is removed) and `TestTwoRelaysPublishEveryRowExactlyOnceWhenNothingCrashes`.

5. **Outbox is at-least-once with a stable message id.** `TestRelayKilledAfterBrokerConfirmButBeforeTheMarkLosesNothingAndRepeatsWithTheSameMessageID`: a relay that dies after the broker confirmed but before the database mark causes a republish of the same message id; consumers must deduplicate (they do: `processed_messages`).

6. **Mutation testing results to be honest about** (details in section 2): 13 of 17 valid mutations were caught. Of the four survivors, three are provably equivalent (`SKIP LOCKED` removal in the saga sweeper and the outbox claim, and the redundant "capture already applied" shortcut) and one (the weakened webhook row lock) is covered by a second layer, the status-guarded update. The new webhook *storm* tests did not catch the removal of both guards; only the two older race tests did. Treat the storm tests as load and regression coverage, not as proof of that race.

7. **A mutation attempt that "passed" was wrong three times and was caught each time by checking that the mutation had actually applied** (a `sed` pattern that did not match, a compile error, an invalid lock strength). Only runs where the mutated line changed and the code compiled are counted.

8. **Contention workloads on the orchestrated path (local, simulated 200 ms gateway, 50 clients, 60 s):** hot product, 80/20 skew and multi-item opposite-order all completed ~19.3/s with end-to-end p95 about 3.1 s — the same number as the uniform baseline, so row contention is not what limits them. The hot-product sell-out ended exactly as it must: 1000 paid, 1000 conflicts (409), no oversell. All invariants passed and there were 0 deadlocks in every run. Lock-waiting sessions peaked at 3.

9. **Throughput is capped well below the old pre-orchestrator numbers, and `WORKER_CONCURRENCY` is only part of the reason.** One variable, 30 clients, 60 s: payment workers 4 -> 19.1 completed/s (p95 2.06 s), 8 -> 25.7/s (p95 1.59 s), 16 -> 24.2/s (p95 1.65 s). Four workers at 0.2 s per job is a 20/s ceiling and the 4-worker result sits on it; doubling to 8 helps by 35 %, but 16 adds nothing, so a second limit near 25/s exists and **has not been diagnosed**. Not a reason to add workers. Candidates (untested): one consumer per service queue (`ORDER_CONSUMER_CONCURRENCY=1`), the outbox/claim poll intervals, the saga's five round trips through RabbitMQ per checkout. Earlier 156/s figures were measured on the removed synchronous design and must not be compared.

10. **5-minute soak (30 clients, workers 4):** 5664 checkouts, all completed, 0 errors, 0 deadlocks, invariants clean, 18.9/s averaged over the run. Whether it decayed during the run is **unknown**: the runner stores only whole-run aggregates (the constant mean in-flight of 30.17 only shows Little's law holds on average). The database grew from 9.3 MB to 55.8 MB (≈ 8 KB per checkout, from sagas, steps, outbox, processed-message and payment rows that are never purged during a run). At that rate the Neon free-tier 512 MB cap is reached after roughly 60,000 checkouts, so any longer Neon run needs the retention jobs or a cleanup.

### Environment problems found

- **The repository-root `.env` is not a valid dotenv file** (byte-order mark, then `Access key ID=…` lines). Every service config loader searches upward for `.env`, so it broke unrelated things: the migrate binaries, the integration tests (until they changed directory), and `docker compose` (any command that reads the project directory). Its parse error **prints the secret values**; the AWS access key and secret in that file have therefore appeared in terminal output several times in this session. **Rotate that key and remove the file's contents from the repo directory.** Workarounds used here: test binaries run from an empty directory (`testing/run-integration.sh`), `COMPOSE_ENV_FILES=/dev/null` for compose, and plain `docker run` for the second replica.
- The loaders should not fail startup on an unreadable parent `.env`, and should never echo its contents in the error.

### Not done, and why

- **Webhook batch / concurrency sweep:** not run. (The worker-concurrency sweep in finding 9 was run instead, with webhooks disabled.) It needs the simulator overlay (`compose.simulator.yaml`), a fresh stack per point, and several minutes per point; the 2026-10-25 choke report already identified `WEBHOOK_PROCESS_BATCH` as the limit and recommends this sweep. It remains the next load experiment.
- **GitHub Actions:** the new `integration` job and the `-race` change are written but could not be executed here; they are untested until the next push.
- **Real Razorpay behaviour:** nothing in this report touches the real gateway (by design: no load against Razorpay).
- **Single replica of every other service**, local Postgres (not Neon), simulated gateway: numbers are not Neon or AWS numbers.
