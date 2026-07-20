package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"

	"golang.org/x/tools/go/packages"
)

// extractFlags: --allow-returns opts into lifting return-bearing blocks into a
// (results..., done bool) helper instead of refusing them. (Package-level var
// initializer — outside gorefactor's function-scoped edit commands.)
var extractFlags = mutFlagSpec(map[string]bool{"--allow-returns": false})

func init() {
	registerCommand(Command{
		Name:        "extract",
		Description: "Extract a code block into a new function (--allow-returns lifts return-bearing blocks). Args: <file> <startLine> <endLine> <methodName>",
		Usage:       "extract <file> <startLine> <endLine> <methodName> [--allow-returns] [--json] [--dry-run] [--gate]",
		MinArgs:     4,
		MaxArgs:     4,
		Flags:       extractFlags,
		Run:         extractCommand,
	})
}

func findFileInPackages(pkgs []*packages.Package, absFile string) (*packages.Package, *ast.File) {
	for _, p := range pkgs {
		for i, f := range p.CompiledGoFiles {
			abs, _ := filepath.Abs(f)
			if abs == absFile {
				if i < len(p.Syntax) {
					return p, p.Syntax[i]
				}
			}
		}
		for i, f := range p.GoFiles {
			abs, _ := filepath.Abs(f)
			if abs == absFile {
				if i < len(p.Syntax) {
					return p, p.Syntax[i]
				}
			}
		}
	}
	return nil, nil
}

func findExtractionTarget(fileAST *ast.File, fset *token.FileSet, startLine, endLine int) (*ast.FuncDecl, []ast.Stmt, error) {
	var enclosing *ast.FuncDecl
	for _, decl := range fileAST.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		s := fset.Position(fn.Body.Lbrace).Line
		e := fset.Position(fn.Body.Rbrace).Line
		if s <= startLine && e >= endLine {
			enclosing = fn
			break
		}
	}
	if enclosing == nil {
		return nil, nil, fmt.Errorf("no function body contains lines %d-%d", startLine, endLine)
	}
	var stmts []ast.Stmt
	for _, stmt := range enclosing.Body.List {
		ss := fset.Position(stmt.Pos()).Line
		se := fset.Position(stmt.End()).Line
		if ss >= startLine && se <= endLine {
			stmts = append(stmts, stmt)
		}
	}
	if len(stmts) == 0 {
		return nil, nil, noStatementsError(fset, enclosing, fset.File(enclosing.Pos()).Name(), startLine, endLine)
	}
	return enclosing, stmts, nil
}

type paramSpec struct {
	name   string
	typeS  string
	object types.Object
	outer  bool // a pre-existing variable the block mutates (write-back at call site with =, not :=)
}

func isLocalToFunc(obj types.Object, fn *ast.FuncDecl, info *types.Info) bool {
	if obj.Parent() == nil {
		return false
	}
	if fn.Type == nil {
		return false
	}
	scope := info.Scopes[fn.Type]
	if scope == nil {
		return false
	}
	for s := obj.Parent(); s != nil; s = s.Parent() {
		if s == scope {
			return true
		}
	}
	if fn.Body != nil {
		if bs, ok := info.Scopes[fn.Body]; ok {
			for s := obj.Parent(); s != nil; s = s.Parent() {
				if s == bs {
					return true
				}
			}
		}
	}
	return false
}

func relativeToPkg(p *types.Package) types.Qualifier {
	return func(other *types.Package) string {
		if other == nil || other == p {
			return ""
		}
		return other.Name()
	}
}

func buildExtractedFunc(fset *token.FileSet, methodName string, stmts []ast.Stmt, params, returns []paramSpec) (newFunc string, callSite string, err error) {
	var body bytes.Buffer
	for i, stmt := range stmts {
		if i > 0 {
			body.WriteString("\n")
		}
		if err := printer.Fprint(&body, fset, stmt); err != nil {
			return "", "", fmt.Errorf("fprint: %w", err)
		}
	}
	newFunc = buildExtractedFuncDecl(methodName, body.String(), params, returns)
	callSite = buildExtractedCallSite(methodName, params, returns)
	return newFunc, callSite, nil

}

func buildExtractedFuncDecl(methodName, body string, params, returns []paramSpec) string {
	var sb strings.Builder
	sb.WriteString("\nfunc ")
	sb.WriteString(methodName)
	sb.WriteString("(")
	for i, p := range params {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s %s", p.name, p.typeS)
	}
	sb.WriteString(")")
	if len(returns) == 1 {
		fmt.Fprintf(&sb, " %s", returns[0].typeS)
	} else if len(returns) > 1 {
		sb.WriteString(" (")
		for i, r := range returns {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(r.typeS)
		}
		sb.WriteString(")")
	}
	sb.WriteString(" {\n")
	sb.WriteString(body)
	if len(returns) > 0 {
		sb.WriteString("\n\treturn ")
		for i, r := range returns {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(r.name)
		}
	}
	sb.WriteString("\n}\n")
	return sb.String()
}

func buildExtractedCallSite(methodName string, params, returns []paramSpec) string {
	var args []string
	for _, p := range params {
		args = append(args, p.name)
	}
	if len(returns) == 0 {
		return fmt.Sprintf("%s(%s)", methodName, strings.Join(args, ", "))
	}
	var names []string
	allOuter := true
	for _, r := range returns {
		names = append(names, r.name)
		if !r.outer {
			allOuter = false
		}
	}

	assign := ":="
	if allOuter {
		assign = "="
	}
	return fmt.Sprintf("%s %s %s(%s)", strings.Join(names, ", "), assign, methodName, strings.Join(args, ", "))
}

func rewriteExtraction(filePath string, fset *token.FileSet, enclosing *ast.FuncDecl, stmts []ast.Stmt, newFunc, callSite string) error {
	src, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	startOff := fset.Position(stmts[0].Pos()).Offset
	endOff := fset.Position(stmts[len(stmts)-1].End()).Offset
	if startOff < 0 || endOff > len(src) || startOff >= endOff {
		return fmt.Errorf("block offset computation failed")
	}
	encEndOff := fset.Position(enclosing.End()).Offset

	var out bytes.Buffer
	out.Write(src[:startOff])
	out.WriteString(callSite)
	out.Write(src[endOff:encEndOff])
	out.WriteString("\n")
	out.WriteString(newFunc)
	out.Write(src[encEndOff:])

	if err := os.WriteFile(filePath, out.Bytes(), 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	if err := orchestrator.FormatImports(filePath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", filePath, err)
	}
	return nil
}
