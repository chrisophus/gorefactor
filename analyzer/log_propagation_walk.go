package analyzer

import (
	"go/ast"
	"go/token"
)

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
		return walkClausesLogReturn(s.Body, logSeen, fset, report)
	case *ast.TypeSwitchStmt:
		return walkClausesLogReturn(s.Body, logSeen, fset, report)
	case *ast.SelectStmt:
		return walkClausesLogReturn(s.Body, logSeen, fset, report)
	case *ast.BlockStmt:
		return walkStmtListLogReturn(s.List, logSeen, fset, report)
	case *ast.LabeledStmt:
		return walkStmtListLogReturn([]ast.Stmt{s.Stmt}, logSeen, fset, report)
	default:
		return logSeen
	}
}

// walkClausesLogReturn walks the statement lists of switch/select clauses
// (both CaseClause and CommClause bodies).
func walkClausesLogReturn(body *ast.BlockStmt, logSeen bool, fset *token.FileSet, report logReportFn) bool {
	for _, c := range body.List {
		switch cc := c.(type) {
		case *ast.CaseClause:
			logSeen = walkStmtListLogReturn(cc.Body, logSeen, fset, report)
		case *ast.CommClause:
			logSeen = walkStmtListLogReturn(cc.Body, logSeen, fset, report)
		}
	}
	return logSeen
}
