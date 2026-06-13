package main

import (
	"fmt"
	goparser "go/parser"
	"go/token"
	"os"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

var replaceBodyFlags = mutFlagSpec(nil)

func init() {
	registerCommand(Command{
		Name:        "replace-body",
		Description: "Replace a function/method body wholesale with new statements from arg or stdin",
		Usage:       "replace-body <file> <Func|Receiver:Method> [content|-] [--json] [--dry-run] [--gate]",
		MinArgs:     2,
		MaxArgs:     3,
		Flags:       replaceBodyFlags,
		Run:         replaceBodyCommand,
	})
}

// replaceBodyCommand swaps the entire body of a target function or method for
// new statements. The content may be a bare statement list or a full `{ ... }`
// block. The result is parse-verified before anything is written, so the
// command refuses to produce malformed Go.
func replaceBodyCommand(args []string) error {
	pos, flags := parseFlags(args, replaceBodyFlags)
	if len(pos) < 2 {
		return usageErrorf("usage: replace-body <file> <Func|Receiver:Method> [content] (else stdin)")
	}
	file := pos[0]
	locator := pos[1]
	m := &mutation{op: "replace-body", file: file}
	m.setCommonFlags(flags)

	content, err := readContentArg(pos, 2)
	if err != nil {
		return m.fail(err)
	}
	block, err := normalizeBodyBlock(content)
	if err != nil {
		return m.fail(err)
	}

	src, err := os.ReadFile(file)
	if err != nil {
		return m.fail(err)
	}
	fset := token.NewFileSet()
	node, err := goparser.ParseFile(fset, file, src, goparser.ParseComments)
	if err != nil {
		return m.fail(parseErrorf("failed to parse %s: %v", file, err))
	}

	funcName, methodName, receiverType := parseLocatorParts(locator)
	ci := orchestrator.NewCodeInserter()
	target := ci.FindFunction(node, funcName, methodName, receiverType)
	if target == nil || target.Body == nil {
		funcs, _ := declNames(node)
		return m.fail(notFoundError(
			fmt.Sprintf("target function %q not found or has no body in %s", locator, file),
			locator, funcs))
	}

	startOffset := fset.Position(target.Body.Lbrace).Offset
	endOffset := fset.Position(target.Body.Rbrace).Offset + 1 // include closing brace
	if startOffset < 0 || endOffset > len(src) || startOffset >= endOffset {
		return m.fail(fmt.Errorf("could not determine function body offsets"))
	}

	var out []byte
	out = append(out, src[:startOffset]...)
	out = append(out, []byte(block)...)
	out = append(out, src[endOffset:]...)

	// Parse-before-write: the whole resulting file must still be valid Go.
	if _, perr := goparser.ParseFile(token.NewFileSet(), file, out, 0); perr != nil {
		return m.fail(parseErrorf("replacement would produce a malformed file: %v", perr))
	}

	return m.run(func() (string, error) {
		if err := os.WriteFile(file, out, 0644); err != nil {
			return "", err
		}
		if err := orchestrator.FormatImports(file); err != nil {
			fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
		}
		return fmt.Sprintf("Replaced body of %s in %s", locator, file), nil
	})
}

// normalizeBodyBlock validates content as either a full `{ ... }` block or a
// bare statement list and returns it as a braced block. Returns an exit-3
// error when neither form parses.
func normalizeBodyBlock(content string) (string, error) {
	parsesAsBody := func(body string) bool {
		_, err := goparser.ParseFile(token.NewFileSet(), "body.go", "package p\nfunc _() "+body, 0)
		return err == nil
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "{\n}", nil
	}
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") && parsesAsBody(trimmed) {
		return trimmed, nil
	}
	block := "{\n" + content + "\n}"
	if parsesAsBody(block) {
		return block, nil
	}
	_, perr := goparser.ParseFile(token.NewFileSet(), "body.go", "package p\nfunc _() "+block, 0)
	return "", parseErrorf("content does not parse as a statement list or function body block: %v", perr)
}
