#!/usr/bin/env bash
# Laptop helper for the cloud tests. It removes the copy-paste mistakes (empty variables, wrong scp target).
#
#   tools/cloud/cloud-test.sh ip                 # current public IP of the load generator
#   tools/cloud/cloud-test.sh new <label>        # start an observability run: prints and remembers the run id
#   tools/cloud/cloud-test.sh push               # copy the load-generator scripts and chaos.py to the load generator
#   tools/cloud/cloud-test.sh ssh                # open a shell on the load generator
#   tools/cloud/cloud-test.sh fetch [run-id]     # close the run, copy results to test-results/<run-id>/, export evidence
#
# If ssh/scp time out, your laptop's public IP changed: add it to the security groups.
set -euo pipefail
cd "$(dirname "$0")/../.."
key="${KEY:-$HOME/.ssh/distri-staging-sydney}"; instance=i-028a4af9b5a71491b; region=ap-southeast-2
state="${XDG_RUNTIME_DIR:-/tmp}/distri-current-run"
lg() {
  local ip
  ip="$(aws ec2 describe-instances --region "$region" --instance-ids "$instance" --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)"
  { [ -n "$ip" ] && [ "$ip" != None ]; } || { echo "load generator has no public IP: is the instance running?" >&2; exit 1; }
  echo "$ip"
}
ssh_opts=(-i "$key" -o BatchMode=yes -o ConnectTimeout=15 -o LogLevel=ERROR)

case "${1:?usage: cloud-test.sh ip|new|push|ssh|fetch}" in
  ip) lg ;;
  new)
    label="${2:?usage: cloud-test.sh new <label>, for example test6}"
    run="cloud-$label-$(date -u +%Y%m%dT%H%M%SZ)"
    deploy/observability/scripts/obs-run.sh start "$run" tunnel staging
    echo "$run" > "$state"; echo; echo "RUN=$run"; echo "(remembered; use this id on the load generator)" ;;
  push)
    ip="$(lg)"
    ssh "${ssh_opts[@]}" "ec2-user@$ip" 'mkdir -p ~/distri/tools/fault-injection ~/distri/tools/cloud'
    scp "${ssh_opts[@]}" tools/fault-injection/chaos.py "ec2-user@$ip:distri/tools/fault-injection/"
    scp "${ssh_opts[@]}" tools/cloud/loadgen-test3.sh tools/cloud/loadgen-load.sh tools/cloud/cloud-set-env.sh tools/cloud/cloud-replica.sh "ec2-user@$ip:distri/tools/cloud/"
    ssh "${ssh_opts[@]}" "ec2-user@$ip" 'chmod +x ~/distri/tools/cloud/*.sh' && echo "pushed to $ip" ;;
  ssh) exec ssh -i "$key" "ec2-user@$(lg)" ;;
  fetch)
    run="${2:-$(cat "$state" 2>/dev/null || true)}"; [ -n "$run" ] || { echo "no run id: pass one" >&2; exit 1; }
    ip="$(lg)"
    deploy/observability/scripts/obs-run.sh stop "$run"
    mkdir -p "test-results/$run"
    # chaos results live in test-results/<run>, load results in test-results/cloud/<run>
    scp -r "${ssh_opts[@]}" "ec2-user@$ip:distri/test-results/$run/." "test-results/$run/" 2>/dev/null || echo "(no chaos results for $run)"
    scp -r "${ssh_opts[@]}" "ec2-user@$ip:distri/test-results/cloud/$run/." "test-results/$run/" 2>/dev/null || echo "(no load results for $run)"
    scp "${ssh_opts[@]}" "ec2-user@$ip:distri/*.log" "test-results/$run/" 2>/dev/null || true
    python3 deploy/observability/scripts/obs-export-run.py "$run"
    echo; echo "results in test-results/$run"; ls "test-results/$run" ;;
  *) echo "usage: cloud-test.sh ip|new <label>|push|ssh|fetch [run-id]" >&2; exit 2 ;;
esac
