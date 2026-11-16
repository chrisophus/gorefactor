This project is GoRefactor, a Go refactoring and analysis tool.

- When a ticket leaves product or design choices open, even ones it says are for a person, pick the
  simplest reasonable option, build it, and list the choices you made at the top of the pull request
  so they can be changed in review. Start permissive and leave tightening for later. Stop and ask only
  when the change would delete data, cannot be undone, or would expose a secret. The other rules here
  still apply.
- Read `AGENTS.md` and `CLAUDE.md` first and follow them. gorefactor is not on your PATH: build it with `go build -o bin/gorefactor ./cmd/gorefactor` and run it as `./bin/gorefactor` when the rules ask you to use it.
- Every change needs a test next to it.
- The structural lint ratchet must not grow: `./bin/gorefactor lint . --baseline --fail-on warning --fail-only` must pass. Never rewrite `.gorefactor-lint-baseline.json` to absorb new findings.
- Run `gofmt -l .`, `go vet ./...` and `go test ./...` for what you changed, and say in your reply what you ran and what it said.
- Add a line to `CHANGELOG.md` for a change a user would notice.
