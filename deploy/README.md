# deploy

Everything needed to run the system outside a laptop: the AWS staging environment the cloud tests ran on, and the monitoring stack that watched it. None of it is needed to run the app locally (use `compose.local.yaml` in the repo root for that).

| Folder | What it is | Start here |
|---|---|---|
| [`cloud/`](cloud) | AWS staging: Terraform for 6 EC2 hosts, per-host compose files, image build and env-file scripts, Neon database provisioning | [`cloud/README.md`](cloud/README.md) |
| [`observability/`](observability) | Prometheus, Grafana, Loki, Tempo, Alertmanager and the per-host agent, plus scripts that label and export a test run | [`observability/README.md`](observability/README.md) |

The scripts that *drive* the cloud tests (start a run, change a setting, add a replica, stop the instances) are in [`../tools/cloud/`](../tools/cloud).

Secrets never live here: `.env*` files, Terraform state and variables, host lists and keys are git-ignored. Only the `*.example` files are committed.
