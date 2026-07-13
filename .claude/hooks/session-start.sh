#!/bin/bash
# SessionStart hook for Claude Code on the web.
#
# This repo's local safety net (the pre-commit hook -> `make gate` ->
# `gorefactor doctor`, which includes a golangci-lint stage) only works if
# (1) the hook is actually wired up in .git/hooks, which is opt-in and lives
# outside version control, so a fresh clone never has it, and (2) a
# golangci-lint binary that's actually able to load this repo's config is on
# PATH — one built with an older Go toolchain than go.mod's `go` directive
# refuses to load the config at all (silently, from `gorefactor doctor`'s
# point of view, since it can't distinguish that from "no findings"). Both
# reset on every fresh container, so this hook re-establishes them each
# session instead of relying on someone remembering a manual one-time step.
set -uo pipefail

REPO_DIR="${CLAUDE_PROJECT_DIR:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
cd "$REPO_DIR" || exit 0

# --- 1. Install the pre-commit hook symlink (idempotent) -------------------
if [ -f .githooks/pre-commit ]; then
	if [ ! -e .git/hooks/pre-commit ]; then
		ln -s ../../.githooks/pre-commit .git/hooks/pre-commit
		echo "session-start: installed .git/hooks/pre-commit -> .githooks/pre-commit"
	elif [ -L .git/hooks/pre-commit ] && [ "$(readlink .git/hooks/pre-commit)" = "../../.githooks/pre-commit" ]; then
		echo "session-start: pre-commit hook already installed"
	else
		echo "session-start: .git/hooks/pre-commit exists and isn't our symlink — leaving it alone"
	fi
fi

# --- 2. Best-effort: install the golangci-lint version pinned in the Makefile
# NOTE: deliberately NOT `go install` — that honors golangci-lint's own
# go.mod/toolchain directive and can produce a binary built with an older Go
# than this repo requires (the exact failure this hook exists to avoid). Only
# the official prebuilt release (built by the golangci-lint maintainers with
# a current Go) is guaranteed usable, hence the install script below.
GOPATH_BIN="$(go env GOPATH 2>/dev/null)/bin"
if [ -n "$GOPATH_BIN" ]; then
	mkdir -p "$GOPATH_BIN"
	case ":$PATH:" in
	*":$GOPATH_BIN:"*) ;;
	*)
		export PATH="$GOPATH_BIN:$PATH"
		if [ -n "${CLAUDE_ENV_FILE:-}" ]; then
			echo "export PATH=\"$GOPATH_BIN:\$PATH\"" >>"$CLAUDE_ENV_FILE"
		fi
		;;
	esac
fi

# version_ge A B: true (0) if dotted-decimal version A >= B.
version_ge() {
	[ "$1" = "$2" ] && return 0
	[ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -n1)" = "$1" ]
}

PINNED_VERSION="$(grep -oE 'GOLANGCI_VERSION *:= *v[0-9.]+' Makefile 2>/dev/null | grep -oE 'v[0-9.]+')"
GO_MOD_VERSION="$(grep -oE '^go [0-9]+\.[0-9]+(\.[0-9]+)?' go.mod 2>/dev/null | awk '{print $2}')"

# golangci_lint_usable: installed, at the pinned version, and built with a Go
# toolchain new enough to load this repo's go.mod-targeted config.
golangci_lint_usable() {
	command -v golangci-lint >/dev/null 2>&1 || return 1
	local ver_line built_go
	ver_line="$(golangci-lint --version 2>/dev/null)"
	echo "$ver_line" | grep -qE "version ${PINNED_VERSION#v}( |$)" || return 1
	built_go="$(echo "$ver_line" | grep -oE 'built with go[0-9]+\.[0-9]+(\.[0-9]+)?' | grep -oE '[0-9]+\.[0-9]+(\.[0-9]+)?')"
	[ -z "$built_go" ] && return 1
	[ -z "$GO_MOD_VERSION" ] && return 0
	version_ge "$built_go" "$GO_MOD_VERSION"
}

if [ -z "$PINNED_VERSION" ]; then
	echo "session-start: could not read GOLANGCI_VERSION from Makefile — skipping golangci-lint install"
elif golangci_lint_usable; then
	echo "session-start: golangci-lint $PINNED_VERSION already installed and usable"
else
	FOUND="$(command -v golangci-lint >/dev/null 2>&1 && golangci-lint --version 2>/dev/null || echo none)"
	echo "session-start: installing golangci-lint $PINNED_VERSION (found: $FOUND)..."
	if curl -sSfL --max-time 30 "https://raw.githubusercontent.com/golangci/golangci-lint/$PINNED_VERSION/install.sh" \
		2>/dev/null | sh -s -- -b "$GOPATH_BIN" "$PINNED_VERSION" >/tmp/golangci-install.log 2>&1; then
		echo "session-start: golangci-lint $PINNED_VERSION installed to $GOPATH_BIN"
	else
		echo "session-start: WARNING - could not install golangci-lint $PINNED_VERSION (network/egress policy likely blocked the release download; see /tmp/golangci-install.log)."
		echo "session-start: 'gorefactor doctor' will report its golangci-lint stage as failing to run rather than silently passing — that's expected here; rely on CI for that check in this session."
	fi
fi

exit 0
