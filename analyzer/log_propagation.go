package analyzer

import (
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

type logReportFn func(pos token.Position, msg string)

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
