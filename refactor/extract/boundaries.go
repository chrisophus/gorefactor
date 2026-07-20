package extract

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// NearestStatementRange scans the enclosing function body for the smallest set
// of complete top-level statements that overlaps the requested [startLine,
// endLine] range. It returns the line span of that set and its statement count.
// ok is false when the function body has no statements at all.
//
// Used to turn the opaque "no complete statements in lines X-Y" rejection into
// an actionable message that names a range the caller can actually extract.
func NearestStatementRange(fset *token.FileSet, fn *ast.FuncDecl, startLine, endLine int) (rStart, rEnd, count int, ok bool) {
	if fn.Body == nil || len(fn.Body.List) == 0 {
		return 0, 0, 0, false
	}
	for _, stmt := range fn.Body.List {
		ss := fset.Position(stmt.Pos()).Line
		se := fset.Position(stmt.End()).Line
		// A statement overlaps the request if it is not entirely before or after it.
		if se < startLine || ss > endLine {
			continue
		}
		if !ok {
			rStart, rEnd = ss, se
			ok = true
		} else {
			if ss < rStart {
				rStart = ss
			}
			if se > rEnd {
				rEnd = se
			}
		}
		count++
	}
	if ok {
		return rStart, rEnd, count, true
	}
	// No statement overlaps: fall back to the first statement after the request
	// so the caller has a concrete valid starting point.
	first := fn.Body.List[0]
	return fset.Position(first.Pos()).Line, fset.Position(first.End()).Line, 1, true
}

// FindJumpBarriers walks the selected statements and reports control-flow jumps
// that would escape an extracted function. A continue/break is a barrier only
// when it is NOT enclosed by its own loop/switch *within* the block: nested
// loops introduced inside the selection capture their own break/continue and are
// safe. return statements are handled separately (DirectReturns).
func FindJumpBarriers(fset *token.FileSet, stmts []ast.Stmt) []JumpBarrier {
	var barriers []JumpBarrier
	for _, top := range stmts {
		walkForBarriers(fset, top, 0, 0, &barriers)
	}
	return barriers
}

// JumpBarrier describes a continue/break/goto/fallthrough statement inside an
// extraction candidate that targets an enclosing scope, making the block
// impossible to extract without restructuring the caller.
type JumpBarrier struct {
	Kind string // "continue", "break", "goto", "fallthrough"
	Line int
}

// noStatementsError builds the boundary-aware replacement for the old opaque
// "no complete statements" rejection.
func noStatementsError(fset *token.FileSet, fn *ast.FuncDecl, file string, startLine, endLine int) error {
	rStart, rEnd, count, ok := NearestStatementRange(fset, fn, startLine, endLine)
	if !ok {
		return fmt.Errorf("no complete statements in lines %d-%d (the enclosing function body is empty)", startLine, endLine)
	}
	return fmt.Errorf(
		"lines %d-%d do not align with statement boundaries. Nearest extractable range: %d-%d (%d statement(s)). "+
			"Try: gorefactor extract %s %d %d <methodName>",
		startLine, endLine, rStart, rEnd, count, file, rStart, rEnd,
	)
}

// walkForBarriers recursively inspects n, tracking how many enclosing loops
// (loopDepth) and switch/select statements (switchDepth) sit between n's root
// and the current node, all *within* the extracted block.
func walkForBarriers(fset *token.FileSet, n ast.Node, loopDepth, switchDepth int, out *[]JumpBarrier) {
	switch s := n.(type) {
	case *ast.ForStmt:
		walkChildren(fset, s.Body, loopDepth+1, switchDepth, out)
		return
	case *ast.RangeStmt:
		walkChildren(fset, s.Body, loopDepth+1, switchDepth, out)
		return
	case *ast.SwitchStmt:
		walkChildren(fset, s.Body, loopDepth, switchDepth+1, out)
		return
	case *ast.TypeSwitchStmt:
		walkChildren(fset, s.Body, loopDepth, switchDepth+1, out)
		return
	case *ast.SelectStmt:
		walkChildren(fset, s.Body, loopDepth, switchDepth+1, out)
		return
	case *ast.BranchStmt:
		barrier := branchBarrier(s, loopDepth, switchDepth)
		if barrier != "" {
			*out = append(*out, JumpBarrier{Kind: barrier, Line: fset.Position(s.Pos()).Line})
		}
		return
	}
	// Generic descent for everything else (if/block/etc.) preserving depths.
	walkChildren(fset, n, loopDepth, switchDepth, out)
}

// branchBarrier classifies a branch statement given the loop/switch nesting that
// is captured inside the extracted block. It returns the barrier kind, or "" if
// the branch is self-contained and therefore safe to extract.
func branchBarrier(s *ast.BranchStmt, loopDepth, switchDepth int) string {
	switch s.Tok {
	case token.CONTINUE:
		if loopDepth == 0 {
			return "continue"
		}
	case token.BREAK:
		// break targets the nearest enclosing loop OR switch/select.
		if loopDepth == 0 && switchDepth == 0 {
			return "break"
		}
	case token.GOTO:
		return "goto" // labels live outside the block; always a barrier.
	case token.FALLTHROUGH:
		if switchDepth == 0 {
			return "fallthrough"
		}
	}
	return ""
}

func walkChildren(fset *token.FileSet, n ast.Node, loopDepth, switchDepth int, out *[]JumpBarrier) {
	if n == nil {
		return
	}
	for _, child := range directChildStmts(n) {
		walkForBarriers(fset, child, loopDepth, switchDepth, out)
	}
}

// directChildStmts returns the statement children of a node one level down, so
// walkForBarriers can control loop/switch depth precisely instead of using
// ast.Inspect (which loses the nesting context).
func directChildStmts(n ast.Node) []ast.Stmt {
	var kids []ast.Stmt
	ast.Inspect(n, func(m ast.Node) bool {
		if m == n {
			return true
		}
		if st, ok := m.(ast.Stmt); ok {
			kids = append(kids, st)
			return false // don't descend; walkForBarriers handles recursion + depth
		}
		return true
	})
	return kids
}

// jumpBarrierError formats the actionable message for a jump-barrier refusal.
func jumpBarrierError(file string, startLine, endLine int, barriers []JumpBarrier) error {
	kinds := make([]string, 0, len(barriers))
	seen := map[string]bool{}
	for _, b := range barriers {
		label := fmt.Sprintf("%s (line %d)", b.Kind, b.Line)
		if !seen[label] {
			kinds = append(kinds, label)
			seen[label] = true
		}
	}
	return fmt.Errorf(
		"lines %d-%d contain a control-flow jump that targets an enclosing scope: %s — "+
			"extraction would require restructuring the caller; convert the jump branches to early "+
			"returns from a helper (e.g. `v, ok := helper(...); if !ok { continue }`), then extract that helper",
		startLine, endLine, strings.Join(kinds, ", "),
	)
}
