<!-- gorefactor:agent-rules -->

## Using gorefactor for .go file changes

When modifying any `.go` file, prefer a gorefactor command over Write or Edit. gorefactor parses the AST, runs goimports, and only writes back well-formed code, so the failure mode is "command rejects the change" rather than "file silently breaks."

Common operations (`gorefactor --help` for the full list):

| Want to… | Use |
|---|---|
| Create a new .go file | `gorefactor create <path> -` (reads stdin) |
| Add a function/type to a file | `gorefactor insert <file> at-end -` |
| Add a function after another | `gorefactor insert <file> after:Func -` |
| Move a function/method | `gorefactor move <src> <Func> <dest>` |
| Replace a complete statement | `gorefactor replace <file> <Func> <old> <new>` |
| Replace text inside a function | `gorefactor replace-text <file> <Func> <old> <new>` |
| Delete a function/method | `gorefactor delete <file> <Func> --safe` |
| Rename an unexported symbol | `gorefactor rename <file> <old> <new>` |
| Split an oversized file | `gorefactor split <file>` |
| Find what calls a function | `gorefactor find-callers <Func>` |
| Find where a symbol is used | `gorefactor find-uses <Symbol>` |
| Find interface implementations | `gorefactor find-implementations <Iface>` |
| Find extraction candidates | `gorefactor recommend <file> --short` |
| Detect code smells | `gorefactor lint .` |
| Final gate (lint + build + test) | `gorefactor doctor` |
| One-page file summary | `gorefactor inspect <file>` |

Methods are referenced as `Receiver:Method` (e.g. `Server:Start`). Pointer receivers work without `*`. Any command that takes content accepts `-` as the last argument to read from stdin (avoids shell quoting on multi-line code).

**Idiomatic Go: accept interfaces, return structs.** Declare interfaces at the *consumer* side (the package that uses them), not at the implementation side. When `gorefactor lint` flags a fat interface or premature abstraction, the idiomatic fix is usually to relocate the interface to the consumer and narrow it to just the methods that consumer needs — not to split it at the declaration site. Use `gorefactor find-implementations <Iface>` to count impls before deciding.

**When Write/Edit is acceptable**: non-Go files (Markdown, YAML, JSON, Makefile, go.mod), or the rare case where no gorefactor command fits. For .go file mutations, fall back to Edit only after confirming no command above applies.
