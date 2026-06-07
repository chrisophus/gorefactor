package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
)

// LogPropagationIssue describes a log-and-return anti-pattern in Go source.
type LogPropagationIssue struct {
	File    string
	Line    int
	Column  int
	Rule    string
	Message string
}

// FileIfErrLogReturnIssues flags if err != nil { log(..., err); return err } patterns.
func FileIfErrLogReturnIssues(file string) ([]LogPropagationIssue, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		return nil, err
	}
	var out []LogPropagationIssue
	report := func(pos token.Position, msg string) {
		out = append(out, LogPropagationIssue{
			File: pos.Filename, Line: pos.Line, Column: pos.Column,
			Rule: "if-err-log-return", Message: msg,
		})
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if stmt, ok := n.(*ast.IfStmt); ok && isErrNotNil(stmt.Cond) {
			walkIfStmtLogReturn(stmt, false, fset, report)
		}
		return true
	})
	return out, nil
}

// FileWrapLogReturnIssues flags err := fmt.Errorf("%w"); log; return err sequences.
func FileWrapLogReturnIssues(file string) ([]LogPropagationIssue, error) {
	return fileBlockLogReturnIssues(file, "wrap-log-return", scanBlockWrapLogReturn)
}

// FileWrapBridgeLogReturnIssues flags wrap→bridge→log→return quads.
func FileWrapBridgeLogReturnIssues(file string) ([]LogPropagationIssue, error) {
	return fileBlockLogReturnIssues(file, "wrap-bridge-log-return", scanBlockWrapBridgeLogReturn)
}

// fileBlockLogReturnIssues parses file and runs a per-block scanner, tagging
// every reported issue with rule. It backs the wrap-log-return and
// wrap-bridge-log-return rules, which differ only in their scanner and rule tag.
func fileBlockLogReturnIssues(
	file, rule string,
	scan func(list []ast.Stmt, fset *token.FileSet, report logReportFn),
) ([]LogPropagationIssue, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		return nil, err
	}
	var out []LogPropagationIssue
	report := func(pos token.Position, msg string) {
		out = append(out, LogPropagationIssue{
			File: pos.Filename, Line: pos.Line, Column: pos.Column,
			Rule: rule, Message: msg,
		})
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if blk, ok := n.(*ast.BlockStmt); ok {
			scan(blk.List, fset, report)
		}
		return true
	})
	return out, nil
}

// PackageDuplicateBareSentinelIssues flags bare returns of the same errors.New sentinel.
func PackageDuplicateBareSentinelIssues(files []string) ([]LogPropagationIssue, error) {
	if len(files) == 0 {
		return nil, nil
	}
	fset := token.NewFileSet()
	var astFiles []*ast.File
	var paths []string
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			continue
		}
		astFiles = append(astFiles, f)
		paths = append(paths, path)
	}
	if len(astFiles) == 0 {
		return nil, nil
	}
	sentinels := collectErrorsNewSentinels(astFiles, paths)
	bare := bareSentinelReturnPositions(astFiles, paths, fset, sentinels)
	var out []LogPropagationIssue
	for name, positions := range bare {
		if len(positions) < 2 {
			continue
		}
		msg := fmt.Sprintf(
			"duplicate bare return of %s (%d sites in package); wrap each with fmt.Errorf(\"…: %%w\", %s)",
			name, len(positions), name)
		for _, pos := range positions {
			out = append(out, LogPropagationIssue{
				File: pos.Filename, Line: pos.Line, Column: pos.Column,
				Rule: "duplicate-bare-sentinel", Message: msg,
			})
		}
	}
	return out, nil
}

type logReportFn func(pos token.Position, msg string)

func walkIfStmtLogReturn(s *ast.IfStmt, logSeen bool, fset *token.FileSet, report logReportFn) bool {
	thenSeen := walkStmtListLogReturn(s.Body.List, logSeen, fset, report)
	if s.Else == nil {
		return thenSeen
	}
	var elseSeen bool
	switch e := s.Else.(type) {
	case *ast.BlockStmt:
		elseSeen = walkStmtListLogReturn(e.List, logSeen, fset, report)
	case *ast.IfStmt:
		elseSeen = walkIfStmtLogReturn(e, logSeen, fset, report)
	}
	return thenSeen || elseSeen
}

func walkStmtListLogReturn(list []ast.Stmt, logSeen bool, fset *token.FileSet, report logReportFn) bool {
	for _, stmt := range list {
		logSeen = walkOneStmtLogReturn(stmt, logSeen, fset, report)
	}
	return logSeen
}

func walkOneStmtLogReturn(stmt ast.Stmt, logSeen bool, fset *token.FileSet, report logReportFn) bool {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		if call, ok := s.X.(*ast.CallExpr); ok && isStructuredLogWithErr(call) {
			return true
		}
		return logSeen
	case *ast.ReturnStmt:
		if !logSeen {
			return logSeen
		}
		switch {
		case isBareReturnErr(s):
			report(fset.Position(s.Pos()), "log then return err")
		case isReturnFmtErrorfWrappingErr(s):
			report(fset.Position(s.Pos()), "log then return fmt.Errorf wrapping err")
		}
		return logSeen
	case *ast.IfStmt:
		return walkIfStmtLogReturn(s, logSeen, fset, report)
	case *ast.ForStmt:
		return walkStmtListLogReturn(s.Body.List, logSeen, fset, report)
	case *ast.RangeStmt:
		return walkStmtListLogReturn(s.Body.List, logSeen, fset, report)
	case *ast.SwitchStmt:
		return walkCaseClausesLogReturn(s.Body, logSeen, fset, report)
	case *ast.TypeSwitchStmt:
		return walkCaseClausesLogReturn(s.Body, logSeen, fset, report)
	case *ast.SelectStmt:
		return walkCommClausesLogReturn(s.Body, logSeen, fset, report)
	case *ast.BlockStmt:
		return walkStmtListLogReturn(s.List, logSeen, fset, report)
	case *ast.LabeledStmt:
		return walkStmtListLogReturn([]ast.Stmt{s.Stmt}, logSeen, fset, report)
	default:
		return logSeen
	}
}

func walkCaseClausesLogReturn(body *ast.BlockStmt, logSeen bool, fset *token.FileSet, report logReportFn) bool {
	for _, c := range body.List {
		cc, ok := c.(*ast.CaseClause)
		if !ok {
			continue
		}
		logSeen = walkStmtListLogReturn(cc.Body, logSeen, fset, report)
	}
	return logSeen
}

func walkCommClausesLogReturn(body *ast.BlockStmt, logSeen bool, fset *token.FileSet, report logReportFn) bool {
	for _, c := range body.List {
		cc, ok := c.(*ast.CommClause)
		if !ok {
			continue
		}
		logSeen = walkStmtListLogReturn(cc.Body, logSeen, fset, report)
	}
	return logSeen
}

func scanBlockWrapLogReturn(list []ast.Stmt, fset *token.FileSet, report logReportFn) {
	for i := 0; i+2 < len(list); i++ {
		if !isAssignErrFmtWrap(list[i]) || !isStructuredLogStmt(list[i+1]) {
			continue
		}
		ret, ok := list[i+2].(*ast.ReturnStmt)
		if !ok || !returnLastIsBareErr(ret) {
			continue
		}
		report(fset.Position(ret.Pos()), "fmt.Errorf wrap then log then return err")
	}
}

func scanBlockWrapBridgeLogReturn(list []ast.Stmt, fset *token.FileSet, report logReportFn) {
	for i := 0; i+3 < len(list); i++ {
		ret, ok := wrapBridgeLogReturnQuadAt(list, i)
		if !ok {
			continue
		}
		report(fset.Position(ret.Pos()), "fmt.Errorf wrap then bridge then log then return same value")
	}
}

func wrapBridgeLogReturnQuadAt(list []ast.Stmt, i int) (*ast.ReturnStmt, bool) {
	if i+3 >= len(list) || !isAssignErrFmtWrap(list[i]) {
		return nil, false
	}
	as, ok := list[i+1].(*ast.AssignStmt)
	if !ok {
		return nil, false
	}
	bridge, ok := singleBridgeAssignName(as)
	if !ok || !exprContainsNamedIdent(as.Rhs[0], "err") {
		return nil, false
	}
	es, ok := list[i+2].(*ast.ExprStmt)
	if !ok {
		return nil, false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok || !isStructuredLogSelector(call) || !callArgsContainNamedIdent(call, bridge) {
		return nil, false
	}
	ret, ok := list[i+3].(*ast.ReturnStmt)
	if !ok || !returnLastIsNamedIdent(ret, bridge) {
		return nil, false
	}
	return ret, true
}

func bareSentinelReturnPositions(files []*ast.File, paths []string, fset *token.FileSet, sentinels map[string]bool) map[string][]token.Position {
	bare := make(map[string][]token.Position)
	for i, f := range files {
		if strings.HasSuffix(paths[i], "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			rs, ok := n.(*ast.ReturnStmt)
			if !ok {
				return true
			}
			for _, r := range rs.Results {
				id, ok := r.(*ast.Ident)
				if !ok || !sentinels[id.Name] {
					continue
				}
				pos := fset.Position(id.Pos())
				bare[id.Name] = append(bare[id.Name], pos)
			}
			return true
		})
	}
	return bare
}

func collectErrorsNewSentinels(files []*ast.File, paths []string) map[string]bool {
	out := make(map[string]bool)
	for i, f := range files {
		if strings.HasSuffix(paths[i], "_test.go") {
			continue
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || vs.Names == nil || len(vs.Values) == 0 {
					continue
				}
				for j := range vs.Names {
					if vs.Names[j].Name == "_" || j >= len(vs.Values) {
						continue
					}
					if isErrorsNewCall(vs.Values[j]) {
						out[vs.Names[j].Name] = true
					}
				}
			}
		}
	}
	return out
}

func isErrNotNil(e ast.Expr) bool {
	be, ok := e.(*ast.BinaryExpr)
	if !ok || be.Op != token.NEQ {
		return false
	}
	return (isErrIdent(be.X) && isNilIdent(be.Y)) || (isErrIdent(be.Y) && isNilIdent(be.X))
}

func isErrIdent(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "err"
}

func isNilIdent(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "nil"
}

func isAssignErrFmtWrap(s ast.Stmt) bool {
	as, ok := s.(*ast.AssignStmt)
	if !ok || len(as.Rhs) != 1 {
		return false
	}
	hasErrLHS := false
	for _, lhs := range as.Lhs {
		if id, ok := lhs.(*ast.Ident); ok && id.Name == "err" {
			hasErrLHS = true
			break
		}
	}
	if !hasErrLHS {
		return false
	}
	call, ok := as.Rhs[0].(*ast.CallExpr)
	return ok && isFmtErrorfWithWrap(call)
}

func isFmtErrorfWithWrap(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Errorf" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "fmt" {
		return false
	}
	for _, a := range call.Args {
		bl, ok := a.(*ast.BasicLit)
		if ok && strings.Contains(bl.Value, "%w") {
			return true
		}
	}
	return false
}

func isStructuredLogSelector(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	n := sel.Sel.Name
	if n != "Error" && n != "Warn" {
		return false
	}
	return !isErrIdent(sel.X)
}

func isStructuredLogWithErr(call *ast.CallExpr) bool {
	if !isStructuredLogSelector(call) {
		return false
	}
	return slices.ContainsFunc(call.Args, containsIdentErr)
}

func isStructuredLogStmt(s ast.Stmt) bool {
	es, ok := s.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	return ok && isStructuredLogWithErr(call)
}

func containsIdentErr(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "err" {
			found = true
			return false
		}
		return true
	})
	return found
}

func exprContainsNamedIdent(e ast.Expr, name string) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

func callArgsContainNamedIdent(call *ast.CallExpr, name string) bool {
	for _, a := range call.Args {
		if exprContainsNamedIdent(a, name) {
			return true
		}
	}
	return false
}

func returnLastIsBareErr(s *ast.ReturnStmt) bool {
	if len(s.Results) != 1 {
		return false
	}
	return isErrIdent(s.Results[0])
}

func returnLastIsNamedIdent(s *ast.ReturnStmt, name string) bool {
	if len(s.Results) == 0 {
		return false
	}
	last := s.Results[len(s.Results)-1]
	id, ok := last.(*ast.Ident)
	return ok && id.Name == name
}

func isBareReturnErr(s *ast.ReturnStmt) bool {
	return len(s.Results) == 1 && isErrIdent(s.Results[0])
}

func isReturnFmtErrorfWrappingErr(s *ast.ReturnStmt) bool {
	if len(s.Results) == 0 {
		return false
	}
	last := s.Results[len(s.Results)-1]
	call, ok := last.(*ast.CallExpr)
	if !ok || !isFmtErrorfWithWrap(call) {
		return false
	}
	return slices.ContainsFunc(call.Args, containsIdentErr)
}

func singleBridgeAssignName(as *ast.AssignStmt) (string, bool) {
	if len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return "", false
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	if !ok || id.Name == "_" {
		return "", false
	}
	return id.Name, true
}

func isErrorsNewCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "New" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "errors"
}
