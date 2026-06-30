# `blast-radius` — function-level change-impact scoring

`blast-radius` answers "how risky is changing this function?" before you touch
it. It is the function-level analog of the package fan-in signal already in the
`high-coupling` lint rule, and was prompted by reviewing
[codeindexer.dev](https://codeindexer.dev/), which surfaces a similar
"blast radius of a change" score (see `docs/llm-compiler-feedback-loops.md` for
the companion review of the ComPilot paper).

## Command

```
gorefactor blast-radius <Func|Receiver:Method> [--in path] [--json]
```

It builds the reverse call graph (reusing the `callgraph` engine,
`buildCallIndex`), walks the transitive caller closure of the target, and
reports:

| field | meaning |
|-------|---------|
| `directCallers` | functions that call the target directly |
| `transitiveCallers` | every function that can reach the target |
| `filesAffected` / `packagesAffected` | breadth of the closure |
| `exported` | whether the symbol is part of the package's public API |
| `score` / `level` | composite risk: `transitive*2 + files + packages + (exported?5)`, bucketed low/medium/high |

```
$ gorefactor blast-radius CodeInserter:InsertCode
blast radius of CodeInserter:InsertCode
  exported:           true
  direct callers:     3
  transitive callers: 17
  files affected:     8
  packages affected:  4
  score:              48  (level: high)
```

## Lint rule: `high-blast-radius`

A matching sensor (rule #26) flags load-bearing functions — `transitiveCallers
>= 20` — at **info** severity (non-blocking; it never fails `lint`/`doctor`).
There is no autofix: the point is awareness ("this is high-impact; make sure
it's tested before you refactor it"), not a mechanical transform. Test
functions are skipped.

## Honest limitation

Like `callgraph` and `find-callers`, call edges are resolved **by name** — a
selector call `x.Foo()` matches a method `Foo` on *any* receiver, because the
index does not carry full type information. Shared method names therefore
**inflate** the counts. Treat the score as a **ranking signal** for "which of
these functions is riskiest to touch," not as an exact dependency count. A
type-resolved version (via `go/packages`, as `cmd_extract.go` already uses)
would tighten this and is the natural follow-up.
