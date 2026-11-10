#!/usr/bin/env bash
# Recreate payment + orchestrator containers (fresh 1 worker / pool 10) and confirm loadgen is idle.
K=~/.ssh/distri-staging-sydney
S() { local ip=$1; shift; for i in 1 2 3 4 5 6 7 8; do ssh -i $K -o ConnectTimeout=15 -o LogLevel=ERROR ec2-user@$ip "$@" && return 0; [ $? -ne 255 ] && return 1; sleep 4; done; return 255; }
S 3.27.207.44 'ps -eo args | grep -qE "[r]un-inprovemts|[r]un-cases" && echo LOADGEN-BUSY || echo loadgen-idle'
S 3.107.25.202 'cd ~/distri && docker compose --env-file deploy/cloud/.env.payment -f compose.cloud.yaml up -d --force-recreate --no-deps --pull never payment-service 2>&1 | tail -1'
S 3.106.230.24 'cd ~/distri && docker compose --env-file deploy/cloud/.env.orchestrator -f compose.cloud.yaml up -d --force-recreate --no-deps --pull never orchestrator 2>&1 | tail -1'
sleep 30; echo fresh-done
