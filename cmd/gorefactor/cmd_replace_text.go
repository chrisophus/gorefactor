package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

// replaceTextCommand performs a literal text substitution inside a target
// function's body. Unlike `replace`, the pattern does not need to be a
// complete statement — it's matched and substituted as raw text. The body
// boundary is enforced so the substitution can't accidentally touch other
// declarations.
func replaceTextCommand(args []string) error {
	if len(args) < 4 {
		return fmt.Errorf("usage: replace-text <file> <funcname-or-Receiver:Method> <old-text> <new-text>")
	}
	file := args[0]
	locator := args[1]
	oldText := args[2]
	newText := args[3]

	src, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, file, src, parser.ParseComments)
	if err != nil {
		return err
	}

	funcName, methodName, receiverType := parseLocatorParts(locator)
	ci := orchestrator.NewCodeInserter()
	target := ci.FindFunction(node, funcName, methodName, receiverType)
	if target == nil || target.Body == nil {
		return fmt.Errorf("target function %q not found or has no body", locator)
	}

	startOffset := fset.Position(target.Body.Lbrace).Offset
	endOffset := fset.Position(target.Body.Rbrace).Offset
	if startOffset < 0 || endOffset > len(src) || startOffset >= endOffset {
		return fmt.Errorf("could not determine function body offsets")
	}
	bodySrc := string(src[startOffset:endOffset])
	if !strings.Contains(bodySrc, oldText) {
		return fmt.Errorf("pattern not found inside %s", locator)
	}
	count := strings.Count(bodySrc, oldText)
	newBody := strings.ReplaceAll(bodySrc, oldText, newText)
	out := append([]byte{}, src[:startOffset]...)
	out = append(out, []byte(newBody)...)
	out = append(out, src[endOffset:]...)
	if err := os.WriteFile(file, out, 0644); err != nil {
		return err
	}
	if err := orchestrator.FormatImports(file); err != nil {
		fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
	}
	fmt.Printf("Replaced %d occurrence(s) in %s\n", count, locator)
	return nil
}

func parseLocatorParts(s string) (funcName, methodName, receiverType string) {
	if i := strings.Index(s, ":"); i >= 0 {
		return "", s[i+1:], s[:i]
	}
	return s, "", ""
}
