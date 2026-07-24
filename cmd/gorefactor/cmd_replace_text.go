package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

var replaceTextFlags = mutFlagSpec(map[string]bool{
	"--first":      false,
	"--occurrence": true,
})

func init() {
	registerCommand(Command{
		Name:        "replace-text",
		Mutates:     true,
		MCPTool:     true,
		TxnSafe:     true,
		Description: "Replace literal text inside a function/method body (safe text find/replace)",
		Usage:       "replace-text <file> <Func|Receiver:Method> <old-text> <new-text> [--first] [--occurrence N] [--json] [--dry-run] [--gate]",
		MinArgs:     4,
		MaxArgs:     4,
		Flags:       replaceTextFlags,
		Run:         replaceTextCommand,
	})
}

// replaceTextCommand performs a literal text substitution inside a target
// function's body. Unlike `replace`, the pattern does not need to be a
// complete statement — it's matched and substituted as raw text. The body
// boundary is enforced so the substitution can't accidentally touch other
// declarations. By default all occurrences are replaced; --first replaces
// only the first and --occurrence N only the Nth.
func replaceTextCommand(args []string) error {
	pos, flags := parseFlags(args, replaceTextFlags)
	if len(pos) < 4 {
		return usageErrorf("usage: replace-text <file> <funcname-or-Receiver:Method> <old-text> <new-text> [--first] [--occurrence N]")
	}
	file, locator, oldText, newText := pos[0], pos[1], pos[2], pos[3]

	m := &mutation{op: "replace-text", file: file}
	m.setCommonFlags(flags)

	occurrence, err := parseReplaceTextOccurrence(flags)
	if err != nil {
		return m.fail(err)
	}

	src, err := os.ReadFile(file)
	if err != nil {
		return m.fail(err)
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, file, src, parser.ParseComments)
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
	endOffset := fset.Position(target.Body.Rbrace).Offset
	if startOffset < 0 || endOffset > len(src) || startOffset >= endOffset {
		return m.fail(fmt.Errorf("could not determine function body offsets"))
	}
	bodySrc := string(src[startOffset:endOffset])
	count := strings.Count(bodySrc, oldText)
	if count == 0 {
		return m.fail(notFoundErrorf("pattern not found inside %s\nhint: verify the exact text exists in the body; to edit text inside a string literal use replace-in-literal, to swap the whole body use replace-body", locator))
	}
	if occurrence > count {
		return m.fail(notFoundErrorf("pattern occurs %d time(s) inside %s; occurrence %d requested", count, locator, occurrence))
	}

	newBody, detail := buildReplacementContent(occurrence, bodySrc, oldText, newText, count, locator)

	return m.run(func() (string, error) {
		return writeReplaceTextResult(file, src, startOffset, endOffset, newBody, detail)
	})
}
func parseReplaceTextOccurrence(flags map[string]string) (int, error) {
	occurrence := 0
	if flags["--first"] != "" {
		occurrence = 1
	}
	if v, ok := flags["--occurrence"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return 0, usageErrorf("--occurrence requires a positive integer, got %q", v)
		}
		occurrence = n
	}
	return occurrence, nil
}

func writeReplaceTextResult(file string, src []byte, startOffset, endOffset int, newBody, detail string) (string, error) {
	out := append([]byte{}, src[:startOffset]...)
	out = append(out, []byte(newBody)...)
	out = append(out, src[endOffset:]...)
	if err := os.WriteFile(file, out, 0644); err != nil {
		return "", err
	}

	if _, perr := parser.ParseFile(token.NewFileSet(), file, out, parser.SkipObjectResolution); perr != nil {
		return "", parseErrorf("replacement would make %s unparseable: %v", file, perr)
	}
	if err := orchestrator.FormatImports(file); err != nil {
		fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
	}
	return detail, nil
}

func buildReplacementContent(occurrence int, bodySrc string, oldText string, newText string, count int, locator string) (string, string) {
	var newBody string
	var detail string
	if occurrence == 0 {
		newBody = strings.ReplaceAll(bodySrc, oldText, newText)
		detail = fmt.Sprintf("Replaced %d occurrence(s) in %s", count, locator)
	} else {
		newBody = replaceNthOccurrence(bodySrc, oldText, newText, occurrence)
		detail = fmt.Sprintf("Replaced occurrence %d of %d in %s", occurrence, count, locator)
	}
	return newBody, detail
}

// replaceNthOccurrence replaces only the n-th (1-based) occurrence of old.
func replaceNthOccurrence(s, old, new string, n int) string {
	offset := 0
	for i := 1; ; i++ {
		idx := strings.Index(s[offset:], old)
		if idx < 0 {
			return s
		}
		idx += offset
		if i == n {
			return s[:idx] + new + s[idx+len(old):]
		}
		offset = idx + len(old)
	}
}
