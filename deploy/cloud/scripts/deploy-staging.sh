#!/usr/bin/env bash
# Deploy a published image tag to the AWS staging hosts, from your laptop, over SSH.
#
#   deploy/cloud/scripts/deploy-staging.sh <image-tag> [--check] [--dry-run] [--smoke] [--yes]
#
#   <image-tag>  the 7-character commit SHA published by the "Publish images" workflow
#   --check      find the hosts and test SSH to each one; change nothing
#   --dry-run    print what would run on each host; connect to nothing
#   --smoke      after deploying, run load case c1 and the phase 22 audit on the load-generator
#                host (this WRITES to the staging database)
#   --yes        do not ask for confirmation
#
# Environment (all optional):
#   SSH_KEY       private key for the instances      (default ~/.ssh/distri-staging-sydney)
#   AWS_REGION    region used to look the hosts up   (default ap-southeast-2)
#   NAME_PREFIX   EC2 Name-tag prefix                (default distri-staging)
#   DISTRI_REPO   image repository                   (default ghcr.io/chrisfonseca8/distri)
#   ORCHESTRATOR_HOST ORDER_HOST INVENTORY_HOST PAYMENT_HOST LOADGEN_HOST
#                 public IPs; when all five are set the AWS lookup is skipped
#
# A connection that times out is retried up to 5 times (a laptop with several internet uplinks
# can leave through an address that is not allowed on port 22); commands are never repeated.
#
# Prerequisites (one time, see deploy/cloud/README.md): the instances are running, the per-host env files
# (deploy/cloud/.env.<orchestrator|order|inventory|payment>, and .env.simulator + .env.loadgen on the
# load-generator host) are on each host, and `docker login ghcr.io` has been run as ec2-user on each
# host unless the packages are public. This script changes only DISTRI_REPO and IMAGE_TAG in
# those env files; no secrets pass through it. The hosts are found with your own local
# `aws` CLI configuration (read-only calls); no AWS keys are stored or created here.
#
# Order, callee first (compose.cloud.yaml): gateway simulator, inventory (migrate + seed),
# payment, order, orchestrator. Roll back by running the script again with the previous tag
# (printed at the end).
set -Eeuo pipefail

usage() { sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//' >&2; exit 2; }

tag="" mode="deploy" smoke="no" assume_yes="no"
for arg in "$@"; do
  case "$arg" in
    --check) mode="check" ;;
    --dry-run) mode="dry-run" ;;
    --smoke) smoke="yes" ;;
    --yes) assume_yes="yes" ;;
    -h|--help) usage ;;
    -*) echo "unknown option: $arg" >&2; usage ;;
    *) [ -z "$tag" ] || { echo "only one image tag allowed" >&2; usage; }; tag="$arg" ;;
  esac
done
[ -n "$tag" ] || { echo "image tag required" >&2; usage; }
echo "$tag" | grep -Eq '^[0-9a-f]{7}$' || { echo "image tag must be a 7-character commit SHA, got '$tag'" >&2; exit 2; }

SSH_KEY="${SSH_KEY:-$HOME/.ssh/distri-staging-sydney}"
AWS_REGION="${AWS_REGION:-ap-southeast-2}"
NAME_PREFIX="${NAME_PREFIX:-distri-staging}"
DISTRI_REPO="$(echo "${DISTRI_REPO:-ghcr.io/chrisfonseca8/distri}" | tr '[:upper:]' '[:lower:]')"
roles=(orchestrator order inventory payment loadgen)

# ---------------------------------------------------------------- hosts
declare -A host
all_overridden="yes"
for r in "${roles[@]}"; do
  var="$(echo "$r" | tr '[:lower:]' '[:upper:]')_HOST"
  if [ -n "${!var:-}" ]; then host[$r]="${!var}"; else all_overridden="no"; fi
done
if [ "$all_overridden" = "no" ]; then
  [ "$mode" != "dry-run" ] || { echo "--dry-run needs the five *_HOST variables (it does not call AWS)" >&2; exit 2; }
  command -v aws >/dev/null || { echo "aws CLI not found; set the five *_HOST variables instead" >&2; exit 2; }
  json="$(aws ec2 describe-instances --region "$AWS_REGION" \
    --filters "Name=tag:Name,Values=${NAME_PREFIX}-*" "Name=instance-state-name,Values=running" \
    --query 'Reservations[].Instances[].[Tags[?Key==`Name`]|[0].Value,PublicIpAddress]' --output text)"
  for r in "${roles[@]}"; do
    ip="$(echo "$json" | awk -v n="${NAME_PREFIX}-$r" '$1==n {print $2}')"
    if [ -z "$ip" ] || [ "$ip" = "None" ]; then
      echo "no running instance with a public IP named ${NAME_PREFIX}-$r (are the instances started?)" >&2; exit 1
    fi
    host[$r]="$ip"
  done
fi

echo "Deploying tag $tag from $DISTRI_REPO"
for r in "${roles[@]}"; do printf '  %-13s %s\n' "$r" "${host[$r]}"; done

# ---------------------------------------------------------------- ssh helpers
ssh_opts=(-i "$SSH_KEY" -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new
          -o ConnectTimeout=10 -o ConnectionAttempts=5 -o ServerAliveInterval=30 -o LogLevel=ERROR)

# remote <role> <label> <script>   (runs as ec2-user on the host; stops the deploy on failure)
remote() {
  local role=$1 label=$2 script=$3
  echo "== $label (${host[$role]})"
  if [ "$mode" = "dry-run" ]; then printf '%s\n' "$script" | sed 's/^/   | /'; return 0; fi
  ssh "${ssh_opts[@]}" "ec2-user@${host[$role]}" bash -s <<< "set -euo pipefail; $script"
}

if [ "$mode" != "dry-run" ]; then
  [ -f "$SSH_KEY" ] || { echo "SSH key not found: $SSH_KEY (set SSH_KEY)" >&2; exit 1; }
  perms="$(stat -c %a "$SSH_KEY")"
  [ "$perms" = "600" ] || [ "$perms" = "400" ] || { echo "SSH key permissions are $perms; run: chmod 600 $SSH_KEY" >&2; exit 1; }
  fail=0
  for r in "${roles[@]}"; do
    case "$r" in
      loadgen) need="deploy/cloud/.env.simulator deploy/cloud/.env.loadgen deploy/cloud/compose.simulator.yaml deploy/cloud/compose.loadgen.yaml" ;;
      *)       need="deploy/cloud/.env.$r compose.cloud.yaml" ;;
    esac
    # connect AND confirm the files the deploy rewrites or reads exist (names only, no contents)
    if out="$(ssh "${ssh_opts[@]}" "ec2-user@${host[$r]}" \
        "cd ~/distri && for f in $need; do test -f \$f || { echo \"missing \$f\"; exit 3; }; done; echo connected" 2>&1)"; then
      echo "ssh ok   $r (files present)"
    else
      echo "ssh FAIL $r: $out"; fail=1
    fi
  done
  if [ "$fail" = 1 ]; then
    echo >&2
    echo "SSH failed. A silent timeout usually means your current public IP ($(curl -s --max-time 5 https://checkip.amazonaws.com || echo unknown))" >&2
    echo "is not allowed on port 22 in the staging security groups. Add it, then retry." >&2
    exit 1
  fi
fi
[ "$mode" != "check" ] || { echo "check complete: nothing was changed"; exit 0; }

# ---------------------------------------------------------------- confirm
if [ "$mode" = "deploy" ] && [ "$assume_yes" != "yes" ]; then
  echo
  echo "This pulls $tag on all five hosts, runs database migrations (and the inventory seed),"
  echo "and restarts the services."
  [ "$smoke" != "yes" ] || echo "It will also run c1 and the audit, which write to the staging database."
  read -r -p "Continue? [y/N] " answer
  [ "$answer" = "y" ] || [ "$answer" = "Y" ] || { echo "aborted"; exit 1; }
fi

# ---------------------------------------------------------------- previous tags (for rollback)
prev=""
if [ "$mode" = "deploy" ]; then
  prev="$(ssh "${ssh_opts[@]}" "ec2-user@${host[orchestrator]}" \
    "grep -m1 '^IMAGE_TAG=' ~/distri/deploy/cloud/.env.orchestrator | cut -d= -f2" 2>/dev/null || true)"
  echo "tag currently deployed on the orchestrator host: ${prev:-unknown}"
fi
on_failure() {
  echo >&2
  echo "DEPLOY FAILED. Hosts earlier in the order may already run $tag." >&2
  [ -z "$prev" ] || echo "To go back: $0 $prev --yes" >&2
}
trap on_failure ERR

# ---------------------------------------------------------------- deploy
set_tag="sed -i \"s|^DISTRI_REPO=.*|DISTRI_REPO=$DISTRI_REPO|; s|^IMAGE_TAG=.*|IMAGE_TAG=$tag|\""   # followed by the env file(s)

remote loadgen "gateway simulator" "
  cd ~/distri
  $set_tag deploy/cloud/.env.simulator deploy/cloud/.env.loadgen
  C=\"docker compose --env-file deploy/cloud/.env.simulator -f deploy/cloud/compose.simulator.yaml\"
  \$C pull
  \$C up -d --wait"

deploy_service() {   # <role> <compose-service> <migrate-service> [extra migrate service]
  local role=$1 svc=$2 migrate=$3 extra=${4:-} extra_cmd=""
  [ -z "$extra" ] || extra_cmd="\$S --profile migrate run --rm $extra"
  remote "$role" "$svc" "
    cd ~/distri
    $set_tag deploy/cloud/.env.$role
    S=\"docker compose --env-file deploy/cloud/.env.$role -f compose.cloud.yaml\"
    \$S pull
    \$S --profile migrate run --rm $migrate
    $extra_cmd
    \$S up -d --wait $svc"
}
deploy_service inventory    inventory-service inventory-migrate inventory-seed
deploy_service payment      payment-service   payment-migrate
deploy_service order        order-service     order-migrate
deploy_service orchestrator orchestrator      orchestrator-migrate

remote loadgen "health checks (from the load-generator host)" "
  cd ~/distri
  set -a; . deploy/cloud/.env.loadgen; set +a
  curl -fsS --max-time 10 \"\$ORDER_URL/health\" >/dev/null && echo order ok
  curl -fsS --max-time 10 \"\$ORCHESTRATOR_URL/readyz\" >/dev/null && echo orchestrator ok
  curl -fsS --max-time 10 http://127.0.0.1:9090/healthz >/dev/null && echo simulator ok"

if [ "$smoke" = "yes" ]; then
  remote loadgen "smoke test c1 + audit (phase 22)" "
    cd ~/distri
    L=\"docker compose --env-file deploy/cloud/.env.loadgen -f deploy/cloud/compose.loadgen.yaml\"
    \$L pull
    \$L --profile loadtest run --rm loadtest
    PHASES=22 \$L --profile correctness run --rm correctness"
fi

trap - ERR
echo
if [ "$mode" = "dry-run" ]; then echo "dry run complete: nothing was executed"; exit 0; fi
echo "Deployed $tag to all five hosts."
[ -z "$prev" ] || echo "Previous tag: $prev   (roll back: $0 $prev --yes)"
echo "Stop the staging instances when you are done; they bill while running."
