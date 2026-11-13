This repository is gorefactor, a Go command-line tool. `CLAUDE.md` at the top of the repository
states its rules and has a map of the packages. Read it before you change anything.

- Build with `make build-fast`, which writes `./bin/gorefactor`. Use its analysis commands
  (`skeleton`, `context`, `find-callers`, `find-uses`) to read code cheaply.
- Every change needs a test next to it. Run `go test` for the packages you changed, then
  `make gate`, and say in your reply what you ran and what it said.
- `make gate` fails when the structural lint baseline grows. Fix the finding. Do not rewrite
  `.gorefactor-lint-baseline.json` to make the gate pass.
- A new lint rule or a changed default also changes a golden test or a doc-drift test. The failing
  test names the file to update.
- Add a line to `CHANGELOG.md` under "Unreleased" for a change a user of the tool would notice.
- You cannot change `.github/` or `.sandbox/`. If the issue needs a change there, make the rest of
  the change and say what is left for a person.
