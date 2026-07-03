#!/usr/bin/env bash
# Delegate a refactoring spec to gorefactor-agent for batch / headless work.
#
# Routing rule (see the cost analysis): the cheap deterministic executor runs
# on Haiku first; if it PUNTS (exit 3 — a capability gap or judgement call),
# escalate to Sonnet. Only pay for the stronger model on a handed-back task.
#
# The agent requires a clean git worktree (it rolls back to that baseline on
# punt), so run this from a committed state. Needs ANTHROPIC_API_KEY.
#
# Usage:
#   scripts/gorefactor-delegate.sh "<spec>" [target-dir]
#   BUDGET=300000 scripts/gorefactor-delegate.sh "extract X into Y"
set -euo pipefail

SPEC="${1:?usage: gorefactor-delegate \"<spec>\" [target-dir]}"
DIR="${2:-.}"
BUDGET="${BUDGET:-200000}"

# Portable: use the globally-installed binary (works in any Go project).
AGENT="$(command -v gorefactor-agent || true)"
if [ -z "$AGENT" ]; then
	echo "gorefactor-agent not on PATH. Install once from the gorefactor repo:" >&2
	echo "  go install ./cmd/gorefactor ./cmd/gorefactor-agent" >&2
	exit 127
fi

run() { "$AGENT" -spec "$SPEC" -provider anthropic -model "$1" -dir "$DIR" -budget "$BUDGET"; }

echo "→ delegating to Haiku (cheap executor)…" >&2
if run "claude-haiku-4-5-20251001"; then
	exit 0
fi
code=$?
if [ "$code" -eq 3 ]; then
	echo "→ Haiku punted; escalating to Sonnet…" >&2
	run "claude-sonnet-4-6"
	exit $?
fi
exit "$code"
