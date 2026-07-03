#!/usr/bin/env bash
# Wire gorefactor tooling into the CURRENT Go project (portable — works in any
# Go module, not just gorefactor itself). Installs:
#   1. agent rules  -> CLAUDE.md / .cursorrules / AGENTS.md
#   2. MCP config   -> .mcp.json (Claude Code) + .cursor/mcp.json (Cursor)
#   3. doctor gate  -> .githooks/pre-commit (lint + build + test on commit)
#
# Prereq: `gorefactor` on PATH (from the gorefactor repo: `make install`).
#
# Usage:
#   cd myproject && gorefactor-init-project            # read-only MCP (safe default)
#   cd myproject && gorefactor-init-project --write     # also expose mutation tools over MCP
set -euo pipefail

command -v gorefactor >/dev/null 2>&1 || {
	echo "gorefactor not on PATH — run 'make install' in the gorefactor repo first." >&2
	exit 127
}
[ -f go.mod ] || { echo "no go.mod here — run from a Go module root." >&2; exit 1; }

WRITE=0
[ "${1:-}" = "--write" ] && WRITE=1

echo "→ writing agent rules + .mcp.json…"
gorefactor init-agent-rules --target all --mcp

write_mcp() { # $1 = destination path
	mkdir -p "$(dirname "$1")"
	if [ "$WRITE" = 1 ]; then
		cat > "$1" <<'JSON'
{
  "mcpServers": {
    "gorefactor": {
      "command": "gorefactor",
      "args": ["mcp", "--allow-write", "--allow-dirty"]
    }
  }
}
JSON
	fi
}

# init-agent-rules already wrote a read-only .mcp.json. Upgrade to write-enabled
# if requested, and mirror the config for Cursor.
[ "$WRITE" = 1 ] && write_mcp ".mcp.json"
cp -f .mcp.json .cursor/mcp.json 2>/dev/null || { mkdir -p .cursor && cp -f .mcp.json .cursor/mcp.json; }
echo "→ Cursor MCP config: .cursor/mcp.json"

echo "→ installing doctor-gate pre-commit hook…"
mkdir -p .githooks
cat > .githooks/pre-commit <<'HOOK'
#!/bin/bash
# gorefactor doctor gate: lint + build + test. Catches broken Go before commit,
# regardless of which tool made the edit. Bypass once with: git commit --no-verify
set -e
command -v gorefactor >/dev/null 2>&1 || { echo "gorefactor not on PATH; skipping gate"; exit 0; }
gorefactor doctor || { echo "✗ doctor gate failed (use 'git commit --no-verify' to bypass)"; exit 1; }
HOOK
chmod +x .githooks/pre-commit

if git rev-parse --git-dir >/dev/null 2>&1; then
	git config core.hooksPath .githooks
	echo "→ git core.hooksPath = .githooks (doctor gate active)"
else
	echo "  (not a git repo — after 'git init', run: git config core.hooksPath .githooks)"
fi

echo "✓ gorefactor wired into '$(basename "$PWD")'  (MCP: $([ "$WRITE" = 1 ] && echo write-enabled || echo read-only))"
