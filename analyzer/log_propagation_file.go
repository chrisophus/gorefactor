package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
)

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
