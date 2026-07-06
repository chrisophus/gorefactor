package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// cmd_edit.go: `edit` unifies the two true near-synonyms — `replace`
// (statement-exact AST match) and `replace-text` (literal body text) —
// behind one verb. They share the exact same shape
// (<file> <locator> <old> <new>); their only difference is whether the
// pattern is a complete statement, which is precisely the distinction
// operators get wrong (the one place a disambiguation hint already lived,
// cmd_direct.go). `edit` tries the deterministic statement match first and
// falls back to body text only when the pattern isn't a complete statement.
//
// It is ADDITIVE, not a replacement: `replace`/`replace-text` remain for
// when you want to pin the scope explicitly, and the genuinely-distinct ops
// (`replace-body` whole-body, `replace-in-literal` file-scoped string
// contents) are NOT folded in — they are different operations, not
// synonyms. Removing the explicit verbs was considered and rejected: their
// schemas are prompt-cached (≈0 per-turn token cost) and there is no
// evidence of selection confusion in the corpus/failure log, so a breaking
// change would be churn with an ambiguity-regression risk.

var editFlags = mutFlagSpec(nil)

func init() {
	registerCommand(Command{
		Name:        "edit",
		Description: "Replace old→new inside a function, auto-selecting statement-exact (replace) or body-text (replace-text) matching",
		Usage:       "edit <file> <Func|Receiver:Method> <old> <new> [--json] [--dry-run] [--gate]",
		MinArgs:     4,
		MaxArgs:     4,
		Flags:       editFlags,
		Run:         editCommand,
	})
}

// editCommand tries a statement-exact replace first; if the pattern isn't a
// complete statement (parse error, or no statement matched), it falls back
// to a body-scoped literal text replace. Any other failure (e.g. function
// not found) is returned as-is rather than masked by the fallback.
func editCommand(args []string) error {
	pos, _ := parseFlags(args, editFlags)
	if len(pos) < 4 {
		return usageErrorf("usage: edit <file> <funcname-or-Receiver:Method> <old> <new>")
	}

	err := replaceCommand(args)
	if err == nil {
		return nil
	}
	if !editShouldFallback(err) {
		return err
	}
	// The pattern was not a complete statement — retry as body text. Note
	// which path we took so the operation of record is unambiguous.
	fmt.Fprintln(os.Stderr, "edit: pattern is not a complete statement; using body-text replace")
	return replaceTextCommand(args)
}

// editShouldFallback reports whether a failed statement-replace should be
// retried as a text replace. It fires only for the "not a complete
// statement" family (snippet parse errors, or no statement matched) — never
// for a genuine target-not-found, which text replace would only repeat.
func editShouldFallback(err error) bool {
	var ce *cliError
	if !errors.As(err, &ce) {
		return false
	}
	if ce.code == exitParseError {
		return true
	}
	return ce.code == exitNotFound && strings.Contains(ce.msg, "statement")
}
