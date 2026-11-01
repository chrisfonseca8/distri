#!/usr/bin/env python3
"""Generate the six Grafana dashboards into grafana/dashboards/*.json.

Re-run after editing: python3 scripts/gen_dashboards.py
Every dashboard has environment, service and test_run_id variables. The matcher F is
appended to metric selectors so each panel obeys them.
"""
import json, os

OUT = os.path.join(os.path.dirname(__file__), "..", "grafana", "dashboards")
PROM = {"type": "prometheus", "uid": "prometheus"}
LOKI = {"type": "loki", "uid": "loki"}
F = 'environment=~"$environment",service=~"$service",test_run_id=~"$test_run_id"'


def sel(metric, extra=""):
    inner = F + ("," + extra if extra else "")
    return metric + "{" + inner + "}"


class Board:
    def __init__(self, uid, title, description):
        self.uid, self.title, self.description = uid, title, description
        self.panels, self._id, self.y = [], 0, 0
        self.col = 0

    def _add(self, p, w, h):
        self._id += 1
        p["id"] = self._id
        if self.col + w > 24:
            self.col, self.y = 0, self.y + self._rowh
        p["gridPos"] = {"x": self.col, "y": self.y, "w": w, "h": h}
        self.col += w
        self._rowh = h
        self.panels.append(p)

    _rowh = 8

    def row(self, title):
        self.col, self.y = 0, self.y + self._rowh
        self._id += 1
        self.panels.append({"id": self._id, "type": "row", "title": title, "collapsed": False,
                            "gridPos": {"x": 0, "y": self.y, "w": 24, "h": 1}})
        self.y += 1
        self._rowh = 0

    def stat(self, title, expr, w=4, h=4, unit="short", red_above=None, desc=""):
        steps = [{"color": "green", "value": None}]
        if red_above is not None:
            steps.append({"color": "red", "value": red_above})
        self._add({"type": "stat", "title": title, "description": desc, "datasource": PROM,
                   "targets": [{"expr": expr, "refId": "A", "datasource": PROM, "instant": True}],
                   "fieldConfig": {"defaults": {"unit": unit, "noValue": "0",
                                                "thresholds": {"mode": "absolute", "steps": steps}}, "overrides": []},
                   "options": {"colorMode": "background" if red_above is not None else "value", "graphMode": "none",
                               "reduceOptions": {"calcs": ["lastNotNull"]}}}, w, h)

    def ts(self, title, exprs, w=8, h=8, unit="short", desc="", stack=False):
        targets = [{"expr": e, "legendFormat": l, "refId": chr(65 + i), "datasource": PROM}
                   for i, (e, l) in enumerate(exprs)]
        custom = {"lineWidth": 1, "fillOpacity": 10, "showPoints": "never"}
        if stack:
            custom["stacking"] = {"mode": "normal"}
        self._add({"type": "timeseries", "title": title, "description": desc, "datasource": PROM,
                   "targets": targets,
                   "fieldConfig": {"defaults": {"unit": unit, "custom": custom}, "overrides": []},
                   "options": {"legend": {"displayMode": "list", "placement": "bottom"},
                               "tooltip": {"mode": "multi"}}}, w, h)

    def table(self, title, expr, w=12, h=8):
        self._add({"type": "table", "title": title, "datasource": PROM,
                   "targets": [{"expr": expr, "refId": "A", "datasource": PROM, "instant": True, "format": "table"}],
                   "transformations": [{"id": "organize", "options": {"excludeByName": {"Time": True}}}]}, w, h)

    def logs(self, title, expr, w=24, h=10):
        self._add({"type": "logs", "title": title, "datasource": LOKI,
                   "targets": [{"expr": expr, "refId": "A", "datasource": LOKI}],
                   "options": {"showTime": True, "wrapLogMessage": True, "sortOrder": "Descending",
                               "enableLogDetails": True}}, w, h)

    def json(self):
        def var(name, query, label):
            return {"name": name, "label": label, "type": "query", "datasource": PROM,
                    "query": {"query": query, "refId": "v"}, "refresh": 2, "includeAll": True,
                    "multi": True, "allValue": ".*", "current": {"text": "All", "value": "$__all"}, "sort": 1}
        return {
            "uid": self.uid, "title": self.title, "description": self.description, "schemaVersion": 39,
            "version": 1, "editable": True, "refresh": "10s", "tags": ["distri"],
            "time": {"from": "now-30m", "to": "now"}, "timezone": "browser",
            "templating": {"list": [
                var("environment", "label_values(up, environment)", "Environment"),
                var("test_run_id", 'label_values(up{environment=~"$environment"}, test_run_id)', "Test run"),
                var("service", 'label_values(up{environment=~"$environment",test_run_id=~"$test_run_id"}, service)', "Service"),
            ]},
            "annotations": {"list": [{"name": "Alerts", "datasource": PROM, "enable": True, "iconColor": "red",
                                      "expr": 'ALERTS{alertstate="firing"}', "titleFormat": "{{alertname}}", "step": "30s"}]},
            "links": [{"title": "Dashboards", "type": "dashboards", "tags": ["distri"], "asDropdown": True}],
            "panels": self.panels}

    def save(self):
        with open(os.path.join(OUT, self.uid + ".json"), "w") as f:
            json.dump(self.json(), f, indent=1)


os.makedirs(OUT, exist_ok=True)

# ---------------------------------------------------------------- 1 overview
b = Board("01-system-overview", "1. System overview", "Is the system healthy right now? Red tiles need attention.")
b.stat("Services up", f'count(up{{job="services",{F}}} == 1)', desc="Scraped application services")
b.stat("Not ready", f'count({sel("service_ready")} == 0)', red_above=1)
b.stat("STUCK sagas", sel('sagas_by_status', 'status="STUCK"').join(["sum(", ")"]), red_above=1)
b.stat("Dead outbox rows", f'sum({sel("outbox_dead_rows")})', red_above=1)
b.stat("Dead-letter queue", f'sum(rabbitmq_detailed_queue_messages{{queue=~".*\\\\.dead",{F}}})', red_above=1)
b.stat("Firing alerts", 'count(ALERTS{alertstate="firing"})', red_above=1)
b.stat("UNCONFIRMED payments", f'sum({sel("payment_attempts_unconfirmed")})', red_above=1)
b.stat("Refunds incomplete", f'sum({sel("payment_refunds_incomplete")})', red_above=1)
b.stat("Oldest outbox row", f'max({sel("outbox_oldest_pending_age_seconds")})', unit="s", red_above=30)
b.stat("Sagas running", f'sum({sel("sagas_by_status", chr(115)+"tatus=\"RUNNING\"")})')
b.stat("Sagas completed", f'sum({sel("sagas_by_status", "status=\"COMPLETED\"")})')
b.stat("Sagas compensated", f'sum({sel("sagas_by_status", "status=\"COMPENSATED\"")})')
b.row("Throughput")
b.ts("Checkouts accepted vs completed (per second)", [
    (f'sum(rate({sel("sagas_started_total")}[1m]))', "accepted (202)"),
    (f'sum(rate({sel("saga_completions_total", "status=\"COMPLETED\"")}[1m]))', "completed"),
    (f'sum(rate({sel("saga_completions_total", "status=\"COMPENSATED\"")}[1m]))', "compensated")], unit="reqps",
     desc="A fast 202 with a growing gap to 'completed' is backlog, not scaling.")
b.ts("Request rate by service", [(f'sum by (service) (rate({sel("http_requests_total")}[1m]))', "{{service}}")], unit="reqps")
b.ts("5xx ratio by service", [(f'sum by (service) (rate({sel("http_requests_total", "code_class=\"5xx\"")}[1m])) / clamp_min(sum by (service) (rate({sel("http_requests_total")}[1m])), 0.001)', "{{service}}")], unit="percentunit")
b.ts("p95 latency by service", [(f'histogram_quantile(0.95, sum by (service, le) (rate({sel("http_request_duration_seconds_bucket")}[1m])))', "{{service}}")], unit="s")
b.ts("Unfinished sagas by status", [(f'sum by (status) ({sel("sagas_by_status")})', "{{status}}")])
b.ts("Alerts firing", [('count by (alertname) (ALERTS{alertstate="firing"})', "{{alertname}}")])
b.row("Events")
b.logs("Key events (stable names)", '{test_run_id=~"$test_run_id", event=~"saga_stuck|outbox_publish_failed|outbox_message_marked_dead|payment_unconfirmed|late_capture_refund_required|stock_invariant_failed"}')
b.save()

# ---------------------------------------------------------------- 2 RED
b = Board("02-service-red", "2. Per-service RED metrics", "Rate, errors and duration for every service, by route.")
b.ts("Request rate by route", [(f'sum by (service, route, method) (rate({sel("http_requests_total")}[1m]))', "{{service}} {{method}} {{route}}")], w=12, unit="reqps")
b.ts("Responses by class", [(f'sum by (service, code_class) (rate({sel("http_requests_total")}[1m]))', "{{service}} {{code_class}}")], w=12, unit="reqps")
b.ts("Error ratio (5xx) by route", [(f'sum by (service, route) (rate({sel("http_requests_total", "code_class=\"5xx\"")}[1m])) / clamp_min(sum by (service, route) (rate({sel("http_requests_total")}[1m])), 0.001)', "{{service}} {{route}}")], w=12, unit="percentunit")
b.ts("Duration p50 / p95 / p99", [
    (f'histogram_quantile({q}, sum by (service, le) (rate({sel("http_request_duration_seconds_bucket")}[1m])))', "{{service}} p" + str(int(q * 100))) for q in (0.5, 0.95, 0.99)], w=12, unit="s")
b.ts("Active requests", [(f'sum by (service) ({sel("http_requests_active")})', "{{service}}")], w=8)
b.ts("Errors by component and kind", [(f'sum by (service, component, kind) (increase({sel("service_errors_total")}[5m]))', "{{service}} {{component}}/{{kind}}")], w=8)
b.ts("Retries by component", [(f'sum by (service, component) (increase({sel("service_retries_total")}[5m]))', "{{service}} {{component}}")], w=8)
b.ts("Outbound HTTP calls by peer and result", [(f'sum by (service, peer, result) (rate({sel("http_client_requests_total")}[1m]))', "{{service}}→{{peer}} {{result}}")], w=12, unit="reqps")
b.ts("Outbound HTTP duration p95", [(f'histogram_quantile(0.95, sum by (service, peer, le) (rate({sel("http_client_request_duration_seconds_bucket")}[1m])))', "{{service}}→{{peer}}")], w=12, unit="s")
b.ts("Goroutines", [(f'{sel("go_goroutines")}', "{{service}}")], w=8)
b.ts("Heap in use", [(f'{sel("go_memstats_heap_inuse_bytes")}', "{{service}}")], w=8, unit="bytes")
b.ts("GC pause (rate of seconds)", [(f'rate({sel("go_gc_duration_seconds_sum")}[1m])', "{{service}}")], w=8, unit="s")
b.save()

# ---------------------------------------------------------------- 3 saga
b = Board("03-saga-flow", "3. Saga flow and completion funnel", "Where checkouts are, how long steps take, and where they get stuck.")
b.stat("Started", f'sum({sel("sagas_started_total")})', w=4)
b.stat("Completed", f'sum({sel("saga_completions_total", "status=\"COMPLETED\"")})', w=4)
b.stat("Compensated", f'sum({sel("saga_completions_total", "status=\"COMPENSATED\"")})', w=4)
b.stat("STUCK", f'sum({sel("sagas_by_status", "status=\"STUCK\"")})', w=4, red_above=1)
b.stat("Oldest active saga", f'max({sel("saga_oldest_active_age_seconds")})', w=4, unit="s")
b.stat("Success ratio", f'sum({sel("saga_completions_total", "status=\"COMPLETED\"")}) / clamp_min(sum({sel("sagas_started_total")}), 1)', w=4, unit="percentunit")
b.ts("Sagas by status", [(f'sum by (status) ({sel("sagas_by_status")})', "{{status}}")], w=8)
b.ts("Unfinished sagas by current step", [(f'sum by (step, status) ({sel("sagas_by_current_step")})', "{{step}} ({{status}})")], w=8, stack=True)
b.ts("Saga completion duration p50 / p95", [
    (f'histogram_quantile({q}, sum by (status, le) (rate({sel("saga_duration_seconds_bucket")}[5m])))', "{{status}} p" + str(int(q * 100))) for q in (0.5, 0.95)], w=8, unit="s")
b.row("Funnel: step outcomes")
b.ts("Step completions by step and outcome (per minute)", [(f'sum by (step, outcome) (rate({sel("saga_step_completions_total")}[2m])) * 60', "{{step}} {{outcome}}")], w=12)
b.table("Funnel (total steps finished Succeeded)", f'sum by (step) ({sel("saga_step_completions_total", "outcome=\"Succeeded\"")})', w=12)
b.ts("Step duration p95", [(f'histogram_quantile(0.95, sum by (step, le) (rate({sel("saga_step_duration_seconds_bucket")}[5m])))', "{{step}}")], w=12, unit="s")
b.ts("Step retries / resends", [(f'sum by (step) (increase({sel("saga_step_retries_total")}[5m]))', "{{step}}")], w=12)
b.row("STUCK and compensation")
b.ts("STUCK transitions by reason", [(f'sum by (reason) (increase({sel("saga_stuck_transitions_total")}[5m]))', "{{reason}}")], w=8)
b.ts("Compensations started", [(f'sum(increase({sel("saga_compensations_started_total")}[5m]))', "started")], w=8)
b.ts("Compensation step outcomes", [(f'sum by (step, outcome) (increase({sel("saga_compensation_steps_total")}[5m]))', "{{step}} {{outcome}}")], w=8)
b.ts("Compensation step duration p95", [(f'histogram_quantile(0.95, sum by (step, le) (rate({sel("saga_compensation_step_duration_seconds_bucket")}[5m])))', "{{step}}")], w=12, unit="s")
b.ts("Steps of unfinished sagas by status", [(f'sum by (status) ({sel("saga_steps_by_status")})', "{{status}}")], w=12)
b.logs("Saga step events", '{service="orchestrator-service", test_run_id=~"$test_run_id"} | json | event=~"saga_step_started|saga_step_completed|saga_stuck"')
b.save()

# ---------------------------------------------------------------- 4 payment & compensation
b = Board("04-payment-compensation", "4. Payment and compensation correctness", "Invariants: no duplicate payment effects, no oversell, every capture refunded or fulfilled.")
b.stat("UNKNOWN attempts", f'sum({sel("payment_attempts_by_status", "status=\"UNKNOWN\"")})', red_above=1)
b.stat("UNCONFIRMED", f'sum({sel("payment_attempts_unconfirmed")})', red_above=1)
b.stat("Refunds incomplete", f'sum({sel("payment_refunds_incomplete")})', red_above=1)
b.stat("Refunds in REVIEW", f'sum({sel("payment_refunds_by_status", "status=\"REVIEW\"")})', red_above=1)
b.stat("Dead jobs", f'sum({sel("payment_jobs", "status=\"dead\"")})', red_above=1)
b.stat("Stock invariant violations", f'sum({sel("inventory_stock_invariant_violations")})', red_above=1)
b.ts("Payment attempts by status", [(f'sum by (status) ({sel("payment_attempts_by_status")})', "{{status}}")])
b.ts("Gateway calls by operation and result", [(f'sum by (operation, result) (rate({sel("payment_gateway_calls_total")}[1m]))', "{{operation}} {{result}}")], unit="reqps",
     desc="Simulator calls are labelled 'sim ...' and are never real gateway throughput.")
b.ts("Gateway call duration p95", [(f'histogram_quantile(0.95, sum by (operation, le) (rate({sel("payment_gateway_call_duration_seconds_bucket")}[2m])))', "{{operation}}")], unit="s")
b.row("Webhooks and jobs")
b.ts("Webhook backlog and parked", [(f'sum({sel("payment_webhook_backlog")})', "pending"), (f'sum({sel("payment_webhook_parked")})', "parked")])
b.ts("Oldest webhook / job age", [(f'max({sel("payment_webhook_oldest_pending_age_seconds")})', "webhook"), (f'max({sel("payment_jobs_oldest_queued_age_seconds")})', "payment job")], unit="s")
b.ts("Payment jobs by status", [(f'sum by (status) ({sel("payment_jobs")})', "{{status}}")])
b.row("Refunds")
b.ts("Refund records by status", [(f'sum by (status) ({sel("payment_refunds_by_status")})', "{{status}}")], w=8)
b.ts("Refunds required (per 5m) by reason", [(f'sum by (reason) (increase({sel("payment_refunds_required_total")}[5m]))', "{{reason}}")], w=8)
b.ts("Oldest incomplete refund", [(f'max({sel("payment_refunds_oldest_incomplete_age_seconds")})', "age")], w=8, unit="s")
b.row("Inventory and orders")
b.ts("Stock units", [(f'sum by (state) ({sel("inventory_units")})', "{{state}}")], w=8)
b.ts("Reservation results", [(f'sum by (result) (increase({sel("inventory_reservations_total")}[5m]))', "{{result}}")], w=8)
b.ts("Reservation duration p95", [(f'histogram_quantile(0.95, sum by (result, le) (rate({sel("inventory_reservation_duration_seconds_bucket")}[2m])))', "{{result}}")], w=8, unit="s")
b.ts("Reservations by status / expired backlog", [(f'sum by (status) ({sel("inventory_reservations_by_status")})', "{{status}}"), (f'sum({sel("inventory_expired_reservation_backlog")})', "expired backlog")], w=8)
b.ts("Orders by status", [(f'sum by (status) ({sel("orders_by_status")})', "{{status}}")], w=8)
b.ts("Order transitions / replays / conflicts", [
    (f'sum by (to) (increase({sel("orders_transitions_total")}[5m]))', "→{{to}}"),
    (f'sum(increase({sel("orders_idempotent_replays_total")}[5m]))', "idempotent replays"),
    (f'sum(increase({sel("orders_conflicts_total")}[5m]))', "conflicts"),
    (f'sum(increase({sel("orders_transition_failures_total")}[5m]))', "failures")], w=8)
b.logs("Correctness events", '{test_run_id=~"$test_run_id", event=~"payment_unconfirmed|late_capture_refund_required|stock_invariant_failed"}')
b.save()

# ---------------------------------------------------------------- 5 rabbit & outbox
b = Board("05-rabbitmq-outbox", "5. RabbitMQ and outbox health", "Message flow between services, retries, dead letters and the outbox relay.")
b.stat("Outbox pending", f'sum({sel("outbox_pending_rows")})', w=4)
b.stat("Outbox dead", f'sum({sel("outbox_dead_rows")})', w=4, red_above=1)
b.stat("Oldest pending", f'max({sel("outbox_oldest_pending_age_seconds")})', w=4, unit="s", red_above=30)
b.stat("Dead-letter queue", f'sum(rabbitmq_detailed_queue_messages{{queue=~".*\\\\.dead",{F}}})', w=4, red_above=1)
b.stat("Consumers connected", f'sum({sel("rabbitmq_consumer_connected")})', w=4)
b.stat("Publish failures (5m)", f'sum(increase({sel("rabbitmq_publish_total", "result!=\"confirmed\"")}[5m]))', w=4, red_above=1)
b.ts("Queue depth (ready)", [(f'sum by (queue) (rabbitmq_detailed_queue_messages_ready{{{F}}})', "{{queue}}")], w=12)
b.ts("Queue depth (unacked)", [(f'sum by (queue) (rabbitmq_detailed_queue_messages_unacked{{{F}}})', "{{queue}}")], w=12)
b.ts("Publish attempts by result", [(f'sum by (service, routing_key, result) (rate({sel("rabbitmq_publish_total")}[1m]))', "{{service}} {{routing_key}} {{result}}")], w=12, unit="ops")
b.ts("Consumed by outcome", [(f'sum by (service, queue, result) (rate({sel("rabbitmq_consume_total")}[1m]))', "{{queue}} {{result}}")], w=12, unit="ops")
b.ts("Consumer processing p95", [(f'histogram_quantile(0.95, sum by (queue, le) (rate({sel("rabbitmq_consume_duration_seconds_bucket")}[2m])))', "{{queue}}")], w=8, unit="s")
b.ts("Retries and dead letters (5m)", [(f'sum by (queue) (increase({sel("rabbitmq_retries_total")}[5m]))', "retry {{queue}}"), (f'sum by (queue, reason) (increase({sel("rabbitmq_dead_letters_total")}[5m]))', "dead {{queue}} {{reason}}")], w=8)
b.ts("Consumers per queue (broker view)", [(f'sum by (queue) (rabbitmq_detailed_queue_consumers{{{F}}})', "{{queue}}")], w=8)
b.row("Outbox")
b.ts("Outbox rows pending / dead", [(f'sum by (service) ({sel("outbox_pending_rows")})', "pending {{service}}"), (f'sum by (service) ({sel("outbox_dead_rows")})', "dead {{service}}")], w=8)
b.ts("Oldest pending outbox row", [(f'{sel("outbox_oldest_pending_age_seconds")}', "{{service}}")], w=8, unit="s")
b.ts("Outbox publish results", [(f'sum by (service, result) (rate({sel("outbox_publish_total")}[1m]))', "{{service}} {{result}}")], w=8, unit="ops")
b.ts("Outbox create→confirm latency p50 / p95", [(f'histogram_quantile({q}, sum by (service, le) (rate({sel("outbox_publish_latency_seconds_bucket")}[2m])))', "{{service}} p" + str(int(q * 100))) for q in (0.5, 0.95)], w=12, unit="s")
b.logs("Outbox events", '{test_run_id=~"$test_run_id", event=~"outbox_publish_failed|outbox_message_marked_dead"}', w=12)
b.save()

# ---------------------------------------------------------------- 6 db & infra
b = Board("06-postgres-infra", "6. Neon/PostgreSQL and infrastructure saturation", "Connection pools, query cost, and host/container resources.")
b.ts("Pool: open / in use / idle", [
    (f'{sel("go_sql_open_connections")}', "open {{service}}"),
    (f'{sel("go_sql_in_use_connections")}', "in use {{service}}"),
    (f'{sel("go_sql_idle_connections")}', "idle {{service}}")], w=8)
b.ts("Pool utilisation (in use / max)", [(f'{sel("go_sql_in_use_connections")} / {sel("go_sql_max_open_connections")}', "{{service}}")], w=8, unit="percentunit")
b.ts("Pool waits (count/s) and wait time (s/s)", [
    (f'rate({sel("go_sql_wait_count_total")}[1m])', "waits {{service}}"),
    (f'rate({sel("go_sql_wait_duration_seconds_total")}[1m])', "wait seconds {{service}}")], w=8)
b.ts("Query duration p95 by operation", [(f'histogram_quantile(0.95, sum by (service, operation, le) (rate({sel("db_query_duration_seconds_bucket")}[2m])))', "{{service}} {{operation}}")], w=12, unit="s")
b.ts("Query rate by operation", [(f'sum by (service, operation) (rate({sel("db_query_duration_seconds_count")}[1m]))', "{{service}} {{operation}}")], w=12, unit="ops")
b.ts("DB errors by kind (deadlock / serialization = transaction failures)", [(f'sum by (service, kind) (increase({sel("db_errors_total")}[5m]))', "{{service}} {{kind}}")], w=12)
b.ts("Transactions by name and result", [(f'sum by (service, name, result) (rate({sel("db_transactions_total")}[1m]))', "{{service}} {{name}} {{result}}")], w=12, unit="ops")
b.ts("Scrape-time gauge query failures", [(f'sum by (service, metric) (increase({sel("metrics_query_errors_total")}[5m]))', "{{service}} {{metric}}")], w=8)
b.row("Hosts and containers (node-exporter / cAdvisor)")
INF = 'environment=~"$environment",test_run_id=~"$test_run_id"'
b.ts("Host CPU busy", [(f'1 - avg by (service) (rate(node_cpu_seconds_total{{mode="idle",{INF}}}[2m]))', "{{service}}")], w=8, unit="percentunit")
b.ts("Host memory used", [(f'1 - node_memory_MemAvailable_bytes{{{INF}}} / node_memory_MemTotal_bytes{{{INF}}}', "{{service}}")], w=8, unit="percentunit")
b.ts("Disk used", [(f'1 - node_filesystem_avail_bytes{{fstype!~"tmpfs|overlay",{INF}}} / node_filesystem_size_bytes{{fstype!~"tmpfs|overlay",{INF}}}', "{{service}} {{mountpoint}}")], w=8, unit="percentunit")
b.ts("Network in/out", [(f'sum by (service) (rate(node_network_receive_bytes_total{{device!~"lo|veth.*|docker.*|br-.*",{INF}}}[1m]))', "in {{service}}"), (f'sum by (service) (rate(node_network_transmit_bytes_total{{device!~"lo|veth.*|docker.*|br-.*",{INF}}}[1m]))', "out {{service}}")], w=8, unit="Bps")
b.ts("Container CPU (cores)", [(f'sum by (service, name) (rate(container_cpu_usage_seconds_total{{name!="",{INF}}}[2m]))', "{{service}} {{name}}")], w=8)
b.ts("Container memory", [(f'sum by (service, name) (container_memory_working_set_bytes{{name!="",{INF}}})', "{{service}} {{name}}")], w=8, unit="bytes")
b.save()
print("wrote", len([f for f in os.listdir(OUT) if f.endswith(".json")]), "dashboards to", os.path.abspath(OUT))
