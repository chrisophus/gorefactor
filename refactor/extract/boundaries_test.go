package extract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestFindJumpBarriers(t *testing.T) {
	const src = `package p
func run(items []int) int {
	total := 0
	for _, x := range items {
		if x < 0 {
			continue
		}
		total += x
	}
	return total
}`
	fset, fd := parseFuncBody(t, src, "run")
	// The if-block containing the continue targets the enclosing for-loop.
	ifStmt := fd.Body.List[1].(*ast.RangeStmt).Body.List[0]
	barriers := FindJumpBarriers(fset, []ast.Stmt{ifStmt})
	if len(barriers) != 1 || barriers[0].Kind != "continue" {
		t.Fatalf("expected one continue barrier, got %+v", barriers)
	}
}

func TestFindJumpBarriers_SelfContainedLoopIsSafe(t *testing.T) {
	const src = `package p
func run(items []int) int {
	total := 0
	for _, x := range items {
		if x < 0 {
			continue
		}
		total += x
	}
	return total
}`
	fset, fd := parseFuncBody(t, src, "run")
	// The whole for-loop captures its own continue — extracting it is safe.
	forStmt := fd.Body.List[1]
	if barriers := FindJumpBarriers(fset, []ast.Stmt{forStmt}); len(barriers) != 0 {
		t.Fatalf("self-contained loop should have no barriers, got %+v", barriers)
	}
}

func TestNearestStatementRange(t *testing.T) {
	const src = `package p
func run() {
	a := 1
	b := 2
	c := a + b
	_ = c
}`
	fset, fd := parseFuncBody(t, src, "run")
	// Request a line inside the body; expect the overlapping statement's span.
	bLine := fset.Position(fd.Body.List[1].Pos()).Line
	rStart, rEnd, count, ok := NearestStatementRange(fset, fd, bLine, bLine)
	if !ok || count != 1 || rStart != bLine || rEnd != bLine {
		t.Fatalf("NearestStatementRange = (%d,%d,%d,%v)", rStart, rEnd, count, ok)
	}
}

func TestSmallExtractionWarning(t *testing.T) {
	const src = `package p
func run() {
	a := 1
	_ = a
}`
	fset, fd := parseFuncBody(t, src, "run")
	// One statement extracted from a requested 10-line range → warn.
	w := SmallExtractionWarning(fset, "helper", fd.Body.List[:1], 3, 12)
	if !strings.Contains(w, "Warning") {
		t.Fatalf("expected small-extraction warning, got %q", w)
	}
	// Two statements over a tight range → no warning.
	if w := SmallExtractionWarning(fset, "helper", fd.Body.List, 3, 4); w != "" {
		t.Fatalf("expected no warning, got %q", w)
	}
}

// parseFuncBody parses src (a full Go file) and returns the fset plus the
// statements of the named function's body.
func parseFuncBody(t *testing.T, src, fn string) (*token.FileSet, *ast.FuncDecl) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == fn {
			return fset, fd
		}
	}
	t.Fatalf("func %s not found", fn)
	return nil, nil
}
