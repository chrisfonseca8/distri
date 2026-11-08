# Test 3 — fault tests on the cloud stack

Run window: 2026-11-01T21:21:23.719605+00:00 → 2026-11-01T21:22:24.757769+00:00 (UTC)<br>
Git:

| Scenario | Result | Accepted | Sagas in flight at fault | Refused while down | Saga status | Unfinished | Duplicates | Dead-queue growth | Drain (s) |
|---|:---:|---:|---:|---:|---|---:|---|---:|---:|
| two-live-orchestrator-replicas | PASS | 200 | - | 0 | {"COMPLETED": 200} | 0 (0 explained) | none | 0 | 11.9 |

## Settings

```json
{
  "target": "cloud",
  "git": "",
  "clients": 30,
  "burst": 200,
  "rate_per_s": 50,
  "kill_after_s": 1.5,
  "settle_timeout_s": 900,
  "fault_target": "payment-service",
  "db_fault_seconds": 20,
  "net_fault_seconds": 40,
  "net_delay": "200ms 50ms"
}
```

A scenario passes only if: at least one checkout was accepted; sagas were in flight when the fault landed; no duplicate payment or reservation; no invariant got worse; every saga is COMPLETED/COMPENSATED (or explained by an UNKNOWN payment where allowed); the dead queue did not grow.
