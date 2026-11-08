#!/usr/bin/env bash
# Test 10 cleanup. Run on the laptop. A DRY RUN unless you pass CONFIRM=yes.
#
#   tools/cloud/cloud-cleanup.sh                  # show what would be stopped and what is running now
#   CONFIRM=yes tools/cloud/cloud-cleanup.sh      # stop every staging instance
#
# It STOPS the instances (reversible: they keep their disks; public IPs change on the next start).
# It does not terminate them and does not touch Terraform state or the Neon branch:
#   - to terminate:   cd deploy/cloud/terraform && terraform plan -destroy   (the broker data volume is
#                     protected by prevent_destroy: removing it needs an explicit Terraform change)
#   - Neon test data: delete the disposable branch in the Neon console, or `neonctl branches delete <name>`
set -euo pipefail
cd "$(dirname "$0")/../.."
region=ap-southeast-2
ids="$(grep -E '^Instance ID:' deploy/cloud/ip_details.txt | awk '{print $3}' | tr '\n' ' ')"
[ -n "$ids" ] || { echo "no instance ids found in deploy/cloud/ip_details.txt" >&2; exit 1; }
echo "instances: $ids"
show() { aws ec2 describe-instances --region "$region" --instance-ids $ids \
  --query 'Reservations[].Instances[].[Tags[?Key==`Name`]|[0].Value,InstanceId,State.Name]' --output text; }
echo "-- state now"; show
if [ "${CONFIRM:-}" != yes ]; then echo; echo "dry run. Run  CONFIRM=yes tools/cloud/cloud-cleanup.sh  to stop them."; exit 0; fi
aws ec2 stop-instances --region "$region" --instance-ids $ids --query 'StoppingInstances[].[InstanceId,CurrentState.Name]' --output text
echo "-- waiting for them to stop"
aws ec2 wait instance-stopped --region "$region" --instance-ids $ids
echo "-- state after"; show
running="$(aws ec2 describe-instances --region "$region" --filters Name=instance-state-name,Values=running,pending --query 'length(Reservations[].Instances[])' --output text)"
echo "instances still running in $region (any project): $running"
