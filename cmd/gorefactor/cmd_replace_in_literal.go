package main

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

var replaceInLiteralFlags = mutFlagSpec(nil)

func init() {
	registerCommand(Command{
		Name:        "replace-in-literal",
		Mutates:     true,
		Description: "Replace a substring inside a single string literal, AST-scoped so surrounding code is never touched",
		Usage:       "replace-in-literal <file> <old> <new> [--json] [--dry-run] [--gate]",
		MinArgs:     3,
		MaxArgs:     3,
		Flags:       replaceInLiteralFlags,
		Run:         replaceInLiteralCommand,
	})
}

// replaceInLiteralCommand replaces old with new inside exactly one string
// literal (interpreted or raw). Unlike replace-text (which is scoped to a
// function body), this reaches string literals anywhere in the file —
// including package-level ones such as a prompt constant — but it is AST
// scoped to a single BasicLit, so it can never corrupt surrounding code. The
// match must be unambiguous: exactly one string literal may contain old.
func replaceInLiteralCommand(args []string) error {
	pos, flags := parseFlags(args, replaceInLiteralFlags)
	if len(pos) < 3 {
		return usageErrorf("usage: replace-in-literal <file> <old> <new>")
	}
	file, oldText, newText := pos[0], pos[1], pos[2]
	if oldText == "" {
		return usageErrorf("old text must be non-empty")
	}

	m := &mutation{op: "replace-in-literal", file: file}
	m.setCommonFlags(flags)

	src, err := os.ReadFile(file)
	if err != nil {
		return m.fail(err)
	}
	fset := token.NewFileSet()
	node, err := goparser.ParseFile(fset, file, src, goparser.ParseComments)
	if err != nil {
		return m.fail(parseErrorf("failed to parse %s: %v", file, err))
	}

	// Find every string literal whose source text contains oldText.
	var matches []*ast.BasicLit
	ast.Inspect(node, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if strings.Contains(lit.Value, oldText) {
			matches = append(matches, lit)
		}
		return true
	})
	switch len(matches) {
	case 0:
		return m.fail(notFoundErrorf("no string literal in %s contains %q\nhint: this command only edits inside string literals; for code (non-string) text use replace-text or `edit`", file, oldText))
	case 1:
		// unambiguous — proceed
	default:
		return m.fail(usageErrorf("%d string literals contain %q; make the pattern unambiguous", len(matches), oldText))
	}

	lit := matches[0]
	litStart := fset.Position(lit.Pos()).Offset
	litEnd := fset.Position(lit.End()).Offset
	newLit := strings.ReplaceAll(lit.Value, oldText, newText)

	out := append([]byte{}, src[:litStart]...)
	out = append(out, []byte(newLit)...)
	out = append(out, src[litEnd:]...)

	if _, perr := goparser.ParseFile(token.NewFileSet(), file, out, 0); perr != nil {
		return m.fail(parseErrorf("the replacement would produce a malformed file: %v", perr))
	}

	return m.run(func() (string, error) {
		if err := os.WriteFile(file, out, 0644); err != nil {
			return "", err
		}
		if err := orchestrator.FormatImports(file); err != nil {
			fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
		}
		return fmt.Sprintf("Replaced text inside a string literal in %s", file), nil
	})
}
