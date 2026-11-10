#!/usr/bin/env bash
# Sequential open-loop runs on the staging load generator, reusing run-cases C4 via compose.loadgen.yaml.
# usage: inprovemts/scripts/sweep.sh <loadgen-ip> <label:rate> ...   e.g. s20:20 s30:30 s40:40
# Records, per run, UTC start/end into inprovemts/data/<label>/times.txt and fetches the runner output.
# Between runs it waits until RabbitMQ ready+unacked = 0 and outbox pending = 0 (from local Prometheus).
set -uo pipefail
ip=$1; shift
K=~/.ssh/distri-staging-sydney
# The laptop's egress IP rotates and not every address is allowed, so every ssh/scp retries.
SSH() { local i; for i in 1 2 3 4 5 6 7 8; do ssh -i "$K" -o ConnectTimeout=12 -o ServerAliveInterval=15 -o LogLevel=ERROR "ec2-user@$ip" "$@" && return 0; [ $? -ne 255 ] && return 1; sleep 3; done; return 255; }
here="$(cd "$(dirname "$0")/.." && pwd)"
prom() { curl -s --data-urlencode "query=$1" localhost:9090/api/v1/query | python3 -c "import sys,json;r=json.load(sys.stdin)['data']['result'];print(sum(float(x['value'][1]) for x in r))"; }
quiet() { # backlog all zero
  [ "$(prom 'sum(rabbitmq_detailed_queue_messages_ready)+sum(rabbitmq_detailed_queue_messages_unacked)')" = "0.0" ] && \
  [ "$(prom 'sum(outbox_pending_rows)')" = "0.0" ] && [ "$(prom 'sum(sagas_by_status{status="RUNNING"})')" = "0.0" ]; }
for spec in "$@"; do
  label=${spec%%:*}; rate=${spec##*:}
  until quiet; do echo "waiting for baseline..."; sleep 10; done
  mkdir -p "$here/data/$label"
  echo "== $label rate=$rate  $(date -u +%FT%TZ)" | tee -a "$here/data/$label/times.txt"
  SSH "nohup ~/run-inprovemts.sh $label $rate ${WARMUP:-60s} ${DURATION:-180s} inprovemts-$label > ~/$label.log 2>&1 &"
  sleep 5
  until SSH "grep -q '^end ' ~/$label.log" 2>/dev/null; do sleep 10; done
  SSH "cat ~/$label.log" | cut -c1-400 > "$here/data/$label/runner.log"
  rn=$(grep -o 'RUN_NAME=.*' "$here/data/$label/runner.log" | head -1 | cut -d= -f2)
  echo "$rn" >> "$here/data/$label/times.txt"; grep -E '^(start|end) ' "$here/data/$label/runner.log" >> "$here/data/$label/times.txt"
  for i in 1 2 3 4 5 6; do scp -q -r -i "$K" -o ConnectTimeout=12 "ec2-user@$ip:distri/test-results/$rn/." "$here/data/$label/" && break; sleep 3; done
  tail -3 "$here/data/$label/runner.log"
done
echo ALLDONE
