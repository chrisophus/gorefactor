#!/usr/bin/env bash
# Sandbox Studio's checks for this repository. Each runs in a slot's worktree as the agent user:
#   build  gofmt, go vet and a build of every package
#   test   the test suite
#   gate   `make gate`: doctor and the structural lint ratchet against the committed baseline
# Go's caches live in the slot, so one slot's build never reads what another slot's agent wrote.
set -euo pipefail
export PATH="/usr/local/go/bin:$PATH"
export GOCACHE="${SANDBOX_SLOT_DIR:?}/gocache" GOMODCACHE="${SANDBOX_SLOT_DIR}/gomod"
case "${1:-}" in
  build)
    bad="$(gofmt -l $(go list -f '{{.Dir}}' ./...))"
    [ -z "$bad" ] || { echo "gofmt would change:" >&2; echo "$bad" >&2; exit 1; }
    go vet ./...
    go build ./...
    ;;
  test) exec go test ./... ;;
  gate) exec make gate ;;
  *) echo "usage: check.sh build|test|gate" >&2; exit 2 ;;
esac
