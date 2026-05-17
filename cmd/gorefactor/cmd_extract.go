package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"github.com/chrisophus/gorefactor/orchestrator"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// extractCommand implements `gorefactor extract <file> <startLine> <endLine> <methodName>`.
// It type-checks the enclosing package, derives parameter and return types
// for the selected block, synthesizes a new function, and rewrites the block
// as a call to it. Designed for free functions and methods at the body level;
// see the doc-comment caveats inside for v1 limitations.
func extractCommand(args []string) error {
	if len(args) < 4 {
		return fmt.Errorf("usage: extract <file> <startLine> <endLine> <methodName>")
	}
	file := args[0]
	startLine, err := strconv.Atoi(args[1])
	if err != nil || startLine < 1 {
		return fmt.Errorf("invalid startLine: %q", args[1])
	}
	endLine, err := strconv.Atoi(args[2])
	if err != nil || endLine < startLine {
		return fmt.Errorf("invalid endLine: %q", args[2])
	}
	methodName := args[3]

	absFile, err := filepath.Abs(file)
	if err != nil {
		return err
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedCompiledGoFiles,
		Dir:   filepath.Dir(absFile),
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return fmt.Errorf("load package: %w", err)
	}
	pkg, fileAST := findFileInPackages(pkgs, absFile)
	if pkg == nil {
		return fmt.Errorf("file %s not in any loaded package", file)
	}
	fset := pkg.Fset

	enclosing, blockStmts, err := findExtractionTarget(fileAST, fset, startLine, endLine)
	if err != nil {
		return err
	}
	if containsReturn(blockStmts) {
		return fmt.Errorf("block contains a return statement; v1 extract does not handle this")
	}

	params, returns, err := analyzeBlockTypes(pkg, fileAST, enclosing, blockStmts)
	if err != nil {
		return err
	}

	newFunc, callSite, err := buildExtractedFunc(fset, methodName, blockStmts, params, returns)
	if err != nil {
		return err
	}

	if err := rewriteExtraction(absFile, fset, enclosing, blockStmts, newFunc, callSite); err != nil {
		return err
	}
	fmt.Printf("Extracted %s (params=%d, returns=%d)\n", methodName, len(params), len(returns))
	return nil
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
		return nil, nil, fmt.Errorf("no complete statements in lines %d-%d (must align with statement boundaries inside the function body)", startLine, endLine)
	}
	return enclosing, stmts, nil
}

func containsReturn(stmts []ast.Stmt) bool {
	for _, s := range stmts {
		found := false
		ast.Inspect(s, func(n ast.Node) bool {
			if _, ok := n.(*ast.ReturnStmt); ok {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

type paramSpec struct {
	name   string
	typeS  string
	object types.Object
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
			return "", "", err
		}
	}

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
	sb.WriteString(body.String())
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

	var args []string
	for _, p := range params {
		args = append(args, p.name)
	}
	if len(returns) == 0 {
		callSite = fmt.Sprintf("%s(%s)", methodName, strings.Join(args, ", "))
	} else {
		var names []string
		for _, r := range returns {
			names = append(names, r.name)
		}
		callSite = fmt.Sprintf("%s := %s(%s)", strings.Join(names, ", "), methodName, strings.Join(args, ", "))
	}
	return sb.String(), callSite, nil
}

func rewriteExtraction(filePath string, fset *token.FileSet, enclosing *ast.FuncDecl, stmts []ast.Stmt, newFunc, callSite string) error {
	src, err := os.ReadFile(filePath)
	if err != nil {
		return err
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
		return err
	}
	if err := orchestrator.FormatImports(filePath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", filePath, err)
	}
	return nil
}
