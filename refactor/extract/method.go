// Package extract is the extract-method engine. It performs the AST analysis,
// type inference, and source rewriting that turns a line range inside a function
// into a call to a synthesized helper, returning computed results and classified
// errors so the CLI, MCP server, and other importers can drive it without
// depending on package main.
package extract

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

	"github.com/chrisophus/gorefactor/internal/cerr"
	"github.com/chrisophus/gorefactor/internal/goload"
	"github.com/chrisophus/gorefactor/orchestrator"

	"golang.org/x/tools/go/packages"
)

// ReturnsRefusedError signals that the selected block contains direct return
// statements and extraction was requested without allowReturns. It carries the
// location detail a caller needs to render a rich, actionable error.
type ReturnsRefusedError struct {
	File        string
	StartLine   int
	EndLine     int
	ReturnLines []int
}

func (e *ReturnsRefusedError) Error() string {
	return "block contains return statement(s); pass --allow-returns to lift them"
}

// TypeAnalysisError wraps a failure to infer parameter/return types for the
// block, tagged with the extraction range so the caller can build a rich error.
type TypeAnalysisError struct {
	File      string
	StartLine int
	EndLine   int
	Err       error
}

func (e *TypeAnalysisError) Error() string { return e.Err.Error() }

func (e *TypeAnalysisError) Unwrap() error { return e.Err }

// MethodPlan is the computed rewrite for an extract-method operation: the
// synthesized helper and its call site, plus the counts and warning the caller
// reports on success.
type MethodPlan struct {
	MethodName    string
	NumParams     int
	NumReturns    int
	LiftedReturns int
	Warning       string

	fset       *token.FileSet
	absFile    string
	enclosing  *ast.FuncDecl
	blockStmts []ast.Stmt
	newFunc    string
	callSite   string
}

// Apply rewrites the block as a call to the synthesized helper and appends the
// helper after the enclosing function, then runs goimports.
func (p *MethodPlan) Apply() error {
	return rewriteExtraction(p.absFile, p.fset, p.enclosing, p.blockStmts, p.newFunc, p.callSite)
}

// PlanMethod type-checks the enclosing package, derives parameter and return
// types for the block spanning [startLine, endLine], and synthesizes a helper
// plus its call site. It performs the same refusals the CLI relies on:
// non-aligned ranges and jump barriers (exit-2 errors), return-bearing blocks
// (*ReturnsRefusedError unless allowReturns), and type-inference failures
// (*TypeAnalysisError).
func PlanMethod(file string, startLine, endLine int, methodName string, allowReturns bool) (*MethodPlan, error) {
	absFile, err := filepath.Abs(file)
	if err != nil {
		return nil, fmt.Errorf("abs: %w", err)
	}
	pkg, fileAST, err := extractLoadTargetPackage(file, absFile)
	if err != nil {
		return nil, fmt.Errorf("extract load target package: %w", err)
	}
	fset := pkg.Fset

	enclosing, blockStmts, err := findExtractionTarget(fileAST, fset, startLine, endLine)
	if err != nil {
		return nil, cerr.NotFoundf("%v", err)
	}

	// Return statements that belong to the block itself (not to function
	// literals inside it) end the enclosing function, so a plain extraction
	// would change behavior. With allowReturns they are lifted into a
	// (results..., done bool) helper instead of refused.
	rets := DirectReturns(blockStmts)
	if len(rets) > 0 && !allowReturns {
		returnLines := make([]int, 0, len(rets))
		for _, r := range rets {
			returnLines = append(returnLines, fset.Position(r.Pos()).Line)
		}
		return nil, &ReturnsRefusedError{File: file, StartLine: startLine, EndLine: endLine, ReturnLines: returnLines}
	}

	// continue/break/goto that target an enclosing scope cannot be extracted
	// without restructuring the caller.
	if barriers := FindJumpBarriers(fset, blockStmts); len(barriers) > 0 {
		return nil, cerr.NotFoundf("%v", jumpBarrierError(file, startLine, endLine, barriers))
	}

	params, returns, err := analyzeBlockTypes(pkg, fileAST, enclosing, blockStmts)
	if err != nil {
		return nil, &TypeAnalysisError{File: file, StartLine: startLine, EndLine: endLine, Err: err}
	}

	newFunc, callSite, err := extractBuildReplacement(fset, absFile, methodName, enclosing, blockStmts, params, returns, rets, startLine, endLine)
	if err != nil {
		return nil, fmt.Errorf("extract build replacement: %w", err)
	}

	return &MethodPlan{
		MethodName:    methodName,
		NumParams:     len(params),
		NumReturns:    len(returns),
		LiftedReturns: len(rets),
		Warning:       SmallExtractionWarning(fset, methodName, blockStmts, startLine, endLine),
		fset:          fset,
		absFile:       absFile,
		enclosing:     enclosing,
		blockStmts:    blockStmts,
		newFunc:       newFunc,
		callSite:      callSite,
	}, nil
}

// SmallExtractionWarning warns when the requested range clips statement
// boundaries and the extractor silently trimmed to a suspiciously small block,
// so the caller can confirm the intended block was taken. Returns "" when the
// extraction looks as requested.
func SmallExtractionWarning(fset *token.FileSet, methodName string, blockStmts []ast.Stmt, startLine, endLine int) string {
	if len(blockStmts) == 0 {
		return ""
	}
	extractedLines := fset.Position(blockStmts[len(blockStmts)-1].End()).Line -
		fset.Position(blockStmts[0].Pos()).Line + 1
	requestedLines := endLine - startLine + 1
	tooFewStmts := len(blockStmts) < 2
	muchSmaller := requestedLines >= 5 && extractedLines*100 < requestedLines*60 // >40% smaller
	if !tooFewStmts && !muchSmaller {
		return ""
	}
	return fmt.Sprintf(
		"Warning: extracted %s contains %d statement(s) spanning %d line(s), but the requested range was %d line(s). "+
			"The range was trimmed to statement boundaries — verify the intended block was captured.",
		methodName, len(blockStmts), extractedLines, requestedLines,
	)
}

func extractLoadTargetPackage(file, absFile string) (*packages.Package, *ast.File, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedCompiledGoFiles,
		Dir:   filepath.Dir(absFile),
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, nil, fmt.Errorf("load package: %w", err)
	}
	pkg, fileAST := goload.FindFileInPackages(pkgs, absFile)
	if pkg == nil {
		return nil, nil, cerr.NotFoundf("file %s not in any loaded package", file)
	}
	return pkg, fileAST, nil
}

func extractBuildReplacement(fset *token.FileSet, absFile, methodName string, enclosing *ast.FuncDecl, blockStmts []ast.Stmt, params, returns []paramSpec, rets []*ast.ReturnStmt, startLine, endLine int) (string, string, error) {
	if len(rets) == 0 {
		return buildExtractedFunc(fset, methodName, blockStmts, params, returns)
	}
	resultTypes, err := enclosingResultTypes(fset, enclosing)
	if err != nil {
		return "", "", err
	}
	if verr := validateReturnLift(fset, rets, len(resultTypes), returns); verr != nil {
		return "", "", cerr.NotFoundf("cannot lift returns in lines %d-%d: %v", startLine, endLine, verr)
	}
	src, err := os.ReadFile(absFile)
	if err != nil {
		return "", "", err
	}

	isTail := blockIsFuncTail(blockStmts, enclosing)
	return buildReturnLiftedFunc(returnLiftSpec{fset: fset, methodName: methodName, stmts: blockStmts, params: params, rets: rets, resultTypes: resultTypes, src: src, isTail: isTail})
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
