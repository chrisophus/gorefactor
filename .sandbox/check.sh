#!/usr/bin/env bash
# The studio's checks for this project (.sandbox/sandbox.yaml): build, lint, test, the same gates
# as CI (.github/workflows/ci.yml). Runs in the slot's worktree as the agent user, so Go's caches
# live in the slot directory and tools are run at CI's pinned versions with `go run`.
set -euo pipefail
export PATH="/usr/local/go/bin:$PATH"
export GOCACHE="${SANDBOX_SLOT_DIR:?}/gocache" GOMODCACHE="${SANDBOX_SLOT_DIR}/gomod"
case "${1:-}" in
  build)
    bad="$(gofmt -l .)"
    [ -z "$bad" ] || { echo "gofmt would change:" >&2; echo "$bad" >&2; exit 1; }
    go vet ./...
    go build ./...
    ;;
  lint)
    go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run ./...
    go build -o "${SANDBOX_SLOT_DIR}/gorefactor" ./cmd/gorefactor
    "${SANDBOX_SLOT_DIR}/gorefactor" lint . --fail-on error
    "${SANDBOX_SLOT_DIR}/gorefactor" lint . --baseline --fail-on warning --fail-only
    ;;
  test) exec go test -race ./... ;;
  *) echo "usage: check.sh build|lint|test" >&2; exit 2 ;;
esac
