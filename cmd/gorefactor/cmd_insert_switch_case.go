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

var insertSwitchCaseFlags = mutFlagSpec(nil)

func init() {
	registerCommand(Command{
		Name:        "insert-switch-case",
		Mutates:     true,
		Description: "Add a case to the switch statement inside a function (before default, else at end)",
		Usage:       "insert-switch-case <file> <Func|Receiver:Method> <case-expr> [body|-] [--json] [--dry-run] [--gate]",
		MinArgs:     3,
		MaxArgs:     4,
		Flags:       insertSwitchCaseFlags,
		Run:         insertSwitchCaseCommand,
	})
}

// insertSwitchCaseCommand adds a new `case <case-expr>: <body>` to the first
// expression switch inside the target function. The case is inserted before
// the default clause when one exists, otherwise as the last clause. Bodies
// and case expressions are spliced as text and the whole file is re-parsed
// and gofmt'd, so indentation need not be exact and malformed input is
// rejected rather than written.
func insertSwitchCaseCommand(args []string) error {
	pos, flags := parseFlags(args, insertSwitchCaseFlags)
	if len(pos) < 3 {
		return usageErrorf("usage: insert-switch-case <file> <Func|Receiver:Method> <case-expr> [body] (else stdin)")
	}
	file, locator, caseExpr := pos[0], pos[1], pos[2]
	if strings.TrimSpace(caseExpr) == "" {
		return usageErrorf("case-expr must be non-empty")
	}

	m := &mutation{op: "insert-switch-case", file: file}
	m.setCommonFlags(flags)

	body, err := readContentArg(pos, 3)
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
	target := orchestrator.NewCodeInserter().FindFunction(node, funcName, methodName, receiverType)
	if target == nil || target.Body == nil {
		return m.fail(notFoundErrorf("target function %q not found or has no body in %s", locator, file))
	}

	sw := firstExprSwitch(target.Body)
	if sw == nil {
		return m.fail(notFoundErrorf("no expression switch found inside %s", locator))
	}

	// Insert before the default clause if present, else before the closing brace.
	insertPos := sw.Body.Rbrace
	if def := defaultClause(sw); def != nil {
		insertPos = def.Case
	}
	insertOff := fset.Position(insertPos).Offset
	if insertOff < 0 || insertOff > len(src) {
		return m.fail(fmt.Errorf("could not determine switch insertion offset"))
	}

	var b strings.Builder
	b.WriteString("case ")
	b.WriteString(strings.TrimSpace(caseExpr))
	b.WriteString(":\n")
	if s := strings.TrimRight(body, "\n"); strings.TrimSpace(s) != "" {
		b.WriteString(s)
		b.WriteString("\n")
	}
	newCase := b.String()

	out := append([]byte{}, src[:insertOff]...)
	out = append(out, []byte(newCase)...)
	out = append(out, src[insertOff:]...)

	if _, perr := goparser.ParseFile(token.NewFileSet(), file, out, 0); perr != nil {
		return m.fail(parseErrorf("inserting the case would produce a malformed file: %v", perr))
	}

	return m.run(func() (string, error) {
		if err := os.WriteFile(file, out, 0644); err != nil {
			return "", err
		}
		if err := orchestrator.FormatImports(file); err != nil {
			fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
		}
		return fmt.Sprintf("Added case %s to the switch in %s (%s)", strings.TrimSpace(caseExpr), locator, file), nil
	})
}

func firstNodeOf[T ast.Node](root ast.Node) T {
	var found T
	var ok bool
	ast.Inspect(root, func(n ast.Node) bool {
		if ok {
			return false
		}
		if t, isT := n.(T); isT {
			found, ok = t, true
			return false
		}
		return true
	})
	return found
}

// firstExprSwitch returns the first expression switch statement (in source
// order) found anywhere inside body, or nil if there is none.
func firstExprSwitch(body *ast.BlockStmt) *ast.SwitchStmt {
	return firstNodeOf[*ast.SwitchStmt](body)

}

// defaultClause returns the switch's default clause, or nil when it has none.
func defaultClause(sw *ast.SwitchStmt) *ast.CaseClause {
	for _, stmt := range sw.Body.List {
		if cc, ok := stmt.(*ast.CaseClause); ok && cc.List == nil {
			return cc
		}
	}
	return nil
}
