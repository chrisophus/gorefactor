package analyzer

import (
	"go/ast"
	"go/token"
)

// DispatchInfo re-scores a function per independent branch. A dispatch table
// — a body dominated by a switch/type-switch whose cases don't interact — is
// read one case at a time, but every function-total metric (lines,
// cyclomatic, cognitive) charges it linearly in the number of cases. The
// normalized scores replace each eligible switch's full contribution with
// "switch frame + its worst single case", which has the units of actual
// reading cost. Rules use it to demote table-shaped findings to info; a
// function whose bulk lies outside its switches, or inside one huge case,
// normalizes high and stays flagged.
type DispatchInfo struct {
	Cases                int `json:"cases"`                // case clauses across eligible top-level switches
	WorstCaseComplexity  int `json:"worstCaseComplexity"`  // complexity points of the heaviest single case body
	WorstCaseLines       int `json:"worstCaseLines"`       // line span of the longest single case body
	NormalizedComplexity int `json:"normalizedComplexity"` // total cyclomatic, each switch re-scored per-branch
	LineDiscount         int `json:"lineDiscount"`         // lines saved by reducing each switch to frame+worst case
}

// AnalyzeDispatch inspects fn's top-level statements for dispatch switches
// and returns the per-branch re-scoring, or nil when the body contains no
// eligible switch. A switch is eligible when it has at least one case and no
// fallthrough (fallthrough makes cases order-dependent, so they can no
// longer be read independently).
func AnalyzeDispatch(fset *token.FileSet, fn *ast.FuncDecl) *DispatchInfo {
	if fn.Body == nil {
		return nil
	}
	info := &DispatchInfo{NormalizedComplexity: calculateFunctionComplexity(fn)}
	found := false
	for _, stmt := range fn.Body.List {
		var body *ast.BlockStmt
		switch s := stmt.(type) {
		case *ast.SwitchStmt:
			body = s.Body
		case *ast.TypeSwitchStmt:
			body = s.Body
		default:
			continue
		}
		if body == nil || len(body.List) == 0 || hasFallthrough(body) {
			continue
		}
		found = true
		scoreDispatchSwitch(fset, stmt, body, info)
	}
	if !found {
		return nil
	}
	return info

}

func scoreDispatchSwitch(fset *token.FileSet, stmt ast.Stmt, body *ast.BlockStmt, info *DispatchInfo) {
	contrib := 0
	countComplexity(stmt, &contrib)
	worstC, worstL, cases := 0, 0, 0
	for _, cs := range body.List {
		cc, ok := cs.(*ast.CaseClause)
		if !ok {
			continue
		}
		cases++
		c, l := 0, caseBodyLines(fset, cc)
		for _, st := range cc.Body {
			countComplexity(st, &c)
		}
		if c > worstC {
			worstC = c
		}
		if l > worstL {
			worstL = l
		}
	}

	info.Cases += cases
	if worstC > info.WorstCaseComplexity {
		info.WorstCaseComplexity = worstC
	}
	if worstL > info.WorstCaseLines {
		info.WorstCaseLines = worstL
	}
	info.NormalizedComplexity += -contrib + 2 + worstC
	span := fset.Position(stmt.End()).Line - fset.Position(stmt.Pos()).Line + 1
	if discount := span - 2 - worstL; discount > 0 {
		info.LineDiscount += discount
	}
}

// caseBodyLines returns the source-line span of one case clause's body.
func caseBodyLines(fset *token.FileSet, cc *ast.CaseClause) int {
	if len(cc.Body) == 0 {
		return 0
	}
	start := fset.Position(cc.Body[0].Pos()).Line
	end := fset.Position(cc.Body[len(cc.Body)-1].End()).Line
	return end - start + 1
}

// hasFallthrough reports whether any case in the switch body ends in a
// fallthrough statement.
func hasFallthrough(body *ast.BlockStmt) bool {
	for _, cs := range body.List {
		cc, ok := cs.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, st := range cc.Body {
			if br, ok := st.(*ast.BranchStmt); ok && br.Tok == token.FALLTHROUGH {
				return true
			}
		}
	}
	return false
}
