#!/usr/bin/env bash
# Sandbox Studio's review gate (ticket_ready.inflight in .sandbox/sandbox.yaml): exits 1 while
# redline-pr-watch, the timer on the studio's host that reviews every pull request
# (ansible-k8s, workstation/redline-pr-watch), is reviewing this one, 0 otherwise. The studio runs
# it in its own clone with GH_REPO set: usage redline-inflight.sh <pr>.
set -euo pipefail
pr="${1:?usage: redline-inflight.sh <pr>}"
[ ! -e "${REDLINE_WATCH_ROOT:-/srv/redline}/inflight/${GH_REPO/\//_}_$pr" ]
