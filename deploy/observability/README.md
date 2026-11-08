# observability

A local monitoring stack for the services, used during the cloud test campaign. Everything listens on `127.0.0.1` only; nothing is opened to the internet. Remote hosts are reached over SSH tunnels.

```
observability/
├── compose.observability.yaml   the stack: Prometheus, Alertmanager, Loki, Tempo, Grafana, collector, node exporter
├── agent/                       runs on every EC2 host beside the services (Alloy config + compose)
├── prometheus/                  prometheus.yml, rules/alerts.yml, targets/ (scrape targets per run)
├── alertmanager/  loki/  tempo/ one config file each
├── grafana/                     dashboards/ (6 JSON dashboards) and provisioning/ (datasources, dashboard loader)
├── scripts/                     label, tunnel and export helpers (below)
└── hosts.env.example            copy to hosts.env (git-ignored) and fill in the EC2 addresses
```

## Run it locally

```bash
docker compose -f deploy/observability/compose.observability.yaml up -d
```

Local ports: Grafana `3000`, Prometheus `9090`, Alertmanager `9095`, Loki `3100`, OTLP `4318`.

## Scripts

| Script | Purpose |
|---|---|
| `obs-run.sh start\|stop <run-id> [local\|tunnel] [env]` | Begin or end a labelled test run and add a Grafana annotation |
| `obs-targets.sh` | Write the Prometheus target files for one run |
| `obs-tunnels.sh` | Open the SSH tunnels to the EC2 hosts (needs `hosts.env`) |
| `obs-export-run.py <run-id>` | Export metrics, logs and the audit of a run into `test-results/<run-id>/` |
| `obs-annotate-chaos.py` | Add Grafana annotations for the faults recorded in a fault-injection result |
| `obs-acceptance.py` | Acceptance test of the stack itself |
| `gen_dashboards.py` | Regenerate the six dashboards in `grafana/dashboards/` |

## Alerts (`prometheus/rules/alerts.yml`)

- **correctness:** `SagaStuck`, `OutboxRowDead`, `DeadLetterQueueGrowing`, `DeadLettersPublished`, `PaymentUnconfirmed`, `RefundRequiredNotCompleted`, `StockInvariantViolated`
- **backlog:** `OutboxBacklogAge`, `WebhookBacklogAge`, `PaymentJobBacklogAge`, `ExpiredReservationBacklog`
- **saturation:** `DatabasePoolSaturated`, `ReadinessFailed`, `ServiceDown`, `HighErrorRate`, `HighLatencyP95`, `HostMemoryPressure`, `HostCPUPressure`, `HostDiskPressure`

Known issue: `DatabasePoolSaturated` fires on any connection-pool wait over a one-minute window as well as on high utilisation, and fired in most load runs including the idle baseline. See [`../../tests/REPORT.md`](../../tests/REPORT.md).
