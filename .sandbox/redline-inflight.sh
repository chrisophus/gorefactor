#!/usr/bin/env bash
# Sandbox Studio's review gate (ticket_ready.inflight in .sandbox/sandbox.yaml): exits 1 while a
# Redline review workflow run is queued or running in this repository, 0 when none is. Asking for
# a review then would only cancel that run (the workflow's concurrency group is per pull request).
set -euo pipefail
busy="$(gh run list -R "$GH_REPO" -w redline -L 20 --json status \
  --jq '[.[] | select(.status != "completed")] | length')"
[ "$busy" = 0 ]
