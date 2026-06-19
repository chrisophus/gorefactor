package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- FileWrapLogReturnIssues ----

func TestFileWrapLogReturnIssues_NoPattern(t *testing.T) {
	t.Parallel()
	src := `package p

func F() error {
	return nil
}
`
	issues, err := FileWrapLogReturnIssues(writeTempGo(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %+v", issues)
	}
}

func TestFileWrapLogReturnIssues_DetectsWrapThenLogThenReturn(t *testing.T) {
	t.Parallel()
	// Pattern: err = fmt.Errorf("...: %w", err); logger.Error("msg", "err", err); return err
	src := `package p

func F(e error) error {
	err := doOp(e)
	if err != nil {
		err = fmt.Errorf("wrap: %w", err)
		logger.Error("op failed", "err", err)
		return err
	}
	return nil
}
`
	issues, err := FileWrapLogReturnIssues(writeTempGo(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) == 0 {
		t.Fatal("expected wrap-log-return issue, got none")
	}
	if issues[0].Rule != "wrap-log-return" {
		t.Errorf("Rule = %q, want %q", issues[0].Rule, "wrap-log-return")
	}
}

// ---- FileWrapBridgeLogReturnIssues ----

func TestFileWrapBridgeLogReturnIssues_NoPattern(t *testing.T) {
	t.Parallel()
	src := `package p

func F() error { return nil }
`
	issues, err := FileWrapBridgeLogReturnIssues(writeTempGo(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %+v", issues)
	}
}

func TestFileWrapBridgeLogReturnIssues_DetectsQuad(t *testing.T) {
	t.Parallel()
	// Pattern: err = fmt.Errorf(…%w…); wrappedErr := someTransform(err); logger.Error(…, wrappedErr); return wrappedErr
	src := `package p

func G(e error) error {
	err = fmt.Errorf("context: %w", e)
	wrappedErr := convert(err)
	logger.Error("failed", "err", wrappedErr)
	return wrappedErr
}
`
	issues, err := FileWrapBridgeLogReturnIssues(writeTempGo(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) == 0 {
		t.Fatal("expected wrap-bridge-log-return issue, got none")
	}
	if issues[0].Rule != "wrap-bridge-log-return" {
		t.Errorf("Rule = %q, want %q", issues[0].Rule, "wrap-bridge-log-return")
	}
}

func TestFileWrapBridgeLogReturnIssues_InvalidFile(t *testing.T) {
	t.Parallel()
	_, err := FileWrapBridgeLogReturnIssues("/nonexistent/path.go")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

// ---- PackageDuplicateBareSentinelIssues ----

func TestPackageDuplicateBareSentinelIssues_EmptyInput(t *testing.T) {
	t.Parallel()
	issues, err := PackageDuplicateBareSentinelIssues(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected no issues for empty input, got %+v", issues)
	}
}

func TestPackageDuplicateBareSentinelIssues_SingleUsage_NoIssue(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	src := `package p

import "errors"

var ErrNotFound = errors.New("not found")

func F() error { return ErrNotFound }
`
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	issues, err := PackageDuplicateBareSentinelIssues([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	// Only one return site — not a duplicate.
	if len(issues) != 0 {
		t.Errorf("expected no issues for single-use sentinel, got %+v", issues)
	}
}

func TestPackageDuplicateBareSentinelIssues_DuplicateUsage_ReturnsIssues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "b.go")
	src := `package p

import "errors"

var ErrBad = errors.New("bad")

func A() error { return ErrBad }
func B() error { return ErrBad }
`
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	issues, err := PackageDuplicateBareSentinelIssues([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) < 2 {
		t.Errorf("expected at least 2 issues for 2 bare returns of ErrBad, got %d: %+v", len(issues), issues)
	}
	for _, iss := range issues {
		if iss.Rule != "duplicate-bare-sentinel" {
			t.Errorf("Rule = %q, want duplicate-bare-sentinel", iss.Rule)
		}
		if !strings.Contains(iss.Message, "ErrBad") {
			t.Errorf("Message missing sentinel name 'ErrBad': %s", iss.Message)
		}
	}
}

func TestPackageDuplicateBareSentinelIssues_SkipsTestFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testFilePath := filepath.Join(dir, "x_test.go")
	src := `package p

import "errors"

var ErrTest = errors.New("test")

func F() error { return ErrTest }
func G() error { return ErrTest }
`
	if err := os.WriteFile(testFilePath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	// Test files should be skipped entirely.
	issues, err := PackageDuplicateBareSentinelIssues([]string{testFilePath})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Errorf("expected no issues because test files are skipped, got %+v", issues)
	}
}

// ---- walkCaseClausesLogReturn ----

func TestWalkCaseClausesLogReturn_SwitchWithLogReturn(t *testing.T) {
	t.Parallel()
	// Manually build an AST switch statement that has log+return in one case.
	src := `package p

func F(x int) error {
	if err != nil {
		switch x {
		case 1:
			logger.Error("case1", "err", err)
			return err
		}
	}
	return nil
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}

	var reports []string
	report := func(pos token.Position, msg string) {
		reports = append(reports, msg)
	}

	ast.Inspect(f, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		// logSeen=true simulates having seen a log call before the switch.
		walkCaseClausesLogReturn(sw.Body, true, fset, report)
		return true
	})

	if len(reports) == 0 {
		t.Error("expected at least one report from switch case with log+return, got none")
	}
}

func TestWalkCaseClausesLogReturn_NoLogSeen(t *testing.T) {
	t.Parallel()
	src := `package p

func F(x int) error {
	switch x {
	case 1:
		return nil
	}
	return nil
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}

	var reports []string
	report := func(pos token.Position, msg string) {
		reports = append(reports, msg)
	}

	ast.Inspect(f, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		walkCaseClausesLogReturn(sw.Body, false, fset, report)
		return true
	})

	if len(reports) != 0 {
		t.Errorf("expected no reports without log, got %v", reports)
	}
}

// ---- walkCommClausesLogReturn ----

func TestWalkCommClausesLogReturn_SelectWithLogReturn(t *testing.T) {
	t.Parallel()
	src := `package p

func F(ch <-chan error) error {
	select {
	case err := <-ch:
		logger.Error("recv", "err", err)
		return err
	}
	return nil
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}

	var reports []string
	report := func(pos token.Position, msg string) {
		reports = append(reports, msg)
	}

	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectStmt)
		if !ok {
			return true
		}
		// logSeen=true to trigger the return check inside the case body.
		walkCommClausesLogReturn(sel.Body, true, fset, report)
		return true
	})

	if len(reports) == 0 {
		t.Error("expected at least one report from select case with log+return")
	}
}

// ---- Low-level helpers ----

func TestIsErrNotNil_Variants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		expr string
		want bool
	}{
		{"err != nil", true},
		{"nil != err", true},
		{"err == nil", false},
		{"x != nil", false},
	}
	for _, c := range cases {
		src := "package p\nvar _ = " + c.expr
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "x.go", src, 0)
		if err != nil {
			continue // malformed, skip
		}
		var got bool
		ast.Inspect(f, func(n ast.Node) bool {
			if be, ok := n.(*ast.BinaryExpr); ok {
				got = isErrNotNil(be)
			}
			return true
		})
		if got != c.want {
			t.Errorf("isErrNotNil(%q) = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestIsErrorsNewCall(t *testing.T) {
	t.Parallel()
	src := `package p

import "errors"

var ErrA = errors.New("a")
var ErrB = fmt.Errorf("b")
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}

	var errorsNewCount, otherCount int
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, v := range vs.Values {
				if isErrorsNewCall(v) {
					errorsNewCount++
				} else {
					otherCount++
				}
			}
		}
	}
	if errorsNewCount != 1 {
		t.Errorf("expected 1 errors.New call, got %d", errorsNewCount)
	}
	if otherCount != 1 {
		t.Errorf("expected 1 non-errors.New call, got %d", otherCount)
	}
}

func TestCollectErrorsNewSentinels(t *testing.T) {
	t.Parallel()
	src := `package p

import "errors"

var ErrFoo = errors.New("foo")
var ErrBar = errors.New("bar")
var NotSentinel = "literal"
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}

	sentinels := collectErrorsNewSentinels([]*ast.File{f}, []string{"x.go"})
	if !sentinels["ErrFoo"] {
		t.Error("expected ErrFoo in sentinels")
	}
	if !sentinels["ErrBar"] {
		t.Error("expected ErrBar in sentinels")
	}
	if sentinels["NotSentinel"] {
		t.Error("did not expect NotSentinel in sentinels")
	}
}

func TestBareSentinelReturnPositions_FindsReturns(t *testing.T) {
	t.Parallel()
	src := `package p

func A() error { return ErrFoo }
func B() error { return ErrFoo }
func C() error { return ErrBar }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "file.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}

	sentinels := map[string]bool{"ErrFoo": true}
	bare := bareSentinelReturnPositions([]*ast.File{f}, []string{"file.go"}, fset, sentinels)
	if len(bare["ErrFoo"]) != 2 {
		t.Errorf("expected 2 positions for ErrFoo, got %d", len(bare["ErrFoo"]))
	}
	if len(bare["ErrBar"]) != 0 {
		t.Errorf("expected 0 positions for non-sentinel ErrBar, got %d", len(bare["ErrBar"]))
	}
}

func TestFileBlockLogReturnIssues_CustomScanner(t *testing.T) {
	t.Parallel()
	// A scanner that reports every statement; verifies fileBlockLogReturnIssues
	// wires the scanner to every block in the file.
	src := `package p

func F() {
	_ = 1
}
`
	count := 0
	scanner := func(list []ast.Stmt, fset *token.FileSet, report logReportFn) {
		count += len(list)
	}
	_, err := fileBlockLogReturnIssues(writeTempGo(t, src), "test-rule", scanner)
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Error("expected scanner to be called with at least one statement")
	}
}
