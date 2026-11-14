This project is GoRefactor, a Go refactoring and analysis tool.

- Read `AGENTS.md` and `CLAUDE.md` first and follow them. gorefactor is not on your PATH: build it into the slot with `go build -o "$SANDBOX_SLOT_DIR/gorefactor" ./cmd/gorefactor` when the rules ask you to use it.
- Every change needs a test next to it.
- The structural lint ratchet must not grow: `"$SANDBOX_SLOT_DIR/gorefactor" lint . --baseline --fail-on warning --fail-only` must pass. Never rewrite `.gorefactor-lint-baseline.json` to absorb new findings.
- Run `gofmt -l .`, `go vet ./...` and `go test ./...` for what you changed, and say in your reply what you ran and what it said.
- Add a line to `CHANGELOG.md` for a change a user would notice.
