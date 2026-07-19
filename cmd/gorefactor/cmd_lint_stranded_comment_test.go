package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisophus/gorefactor/analyzer"
)

// strandedFixtureSrc reproduces the pre-repair orchestrator/types.go failure
// mode: a mechanical edit moved declarations out from under their doc
// comments, leaving each comment attached to the wrong sibling.
const strandedFixtureSrc = `package fixture

// RefactoringPlan is a complete plan of operations to execute in order.
type Operation struct {
	Kind string
}

// Operation is one step of a plan.
type RefactoringPlan struct {
	Ops []Operation
}

// Execute runs every operation in the plan.
func validate(p RefactoringPlan) error {
	return nil
}

// Execute really does run the plan; its own doc comment is correct.
func Execute(p RefactoringPlan) error {
	return validate(p)
}
`

func TestStrandedComment_FiresOnPreRepairFixture(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "types.go")
	if err := os.WriteFile(path, []byte(strandedFixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := strandedCommentRule{}.Run(LintContext{Root: dir, Files: []string{path}})
	if len(issues) != 3 {
		t.Fatalf("got %d issues, want 3 (RefactoringPlan-comment on Operation, Operation-comment on RefactoringPlan, Execute-comment on validate): %+v", len(issues), issues)
	}
	for _, iss := range issues {
		if iss.Rule != "stranded-comment" || iss.Severity != "warning" {
			t.Errorf("issue has rule=%q severity=%q, want stranded-comment/warning", iss.Rule, iss.Severity)
		}
	}
	// The correctly-documented Execute must not be flagged.
	for _, iss := range issues {
		if strings.Contains(iss.Message, `opens with "Execute" but documents func Execute`) {
			t.Errorf("correctly-attached Execute doc was flagged: %s", iss.Message)
		}
	}
}

// TestStrandedComment_CleanCasesStaySilent pins the precision guards: own-name
// comments, prose openers, Package/Deprecated openers, identifiers that name
// nothing in the package, and test files are all ignored.
func TestStrandedComment_CleanCasesStaySilent(t *testing.T) {
	dir := t.TempDir()
	clean := `package fixture

// Alpha does one thing.
func Alpha() {}

// The helper below supports Alpha.
func beta() {}

// Package-style prose never counts as an identifier reference.
func gamma() {}

// Deprecated: use Alpha instead.
func Delta() {}

// UnknownName is not declared anywhere in this package.
func epsilon() {}
`
	path := filepath.Join(dir, "clean.go")
	if err := os.WriteFile(path, []byte(clean), 0o644); err != nil {
		t.Fatal(err)
	}
	// A test file with a stranded-shaped comment must be skipped entirely.
	testPath := filepath.Join(dir, "clean_test.go")
	testSrc := `package fixture

// Alpha mirrors the fixer: conventional test-comment style.
func TestAlpha(t *testing.T) {}
`
	if err := os.WriteFile(testPath, []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := strandedCommentRule{}.Run(LintContext{Root: dir, Files: []string{path, testPath}})
	if len(issues) != 0 {
		t.Fatalf("clean fixture produced %d issues: %+v", len(issues), issues)
	}
}

// TestStrandedComment_ZeroFindingsOnTree is the plan item's acceptance
// criterion: the rule must be silent on the current repository. The two
// stranded comments it found on its first run (dispatch_tool.go,
// run_interactive_agentic_driver.go) were fixed in the same change.
func TestStrandedComment_ZeroFindingsOnTree(t *testing.T) {
	files, err := analyzer.WalkGoFiles("../..", analyzer.DefaultWalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	issues := strandedCommentRule{}.Run(LintContext{Root: "../..", Files: files})
	for _, iss := range issues {
		t.Errorf("stranded comment in tree: %s: %s", iss.File, iss.Message)
	}
}

const freeFloatingFixtureSrc = `package fixture

// executeMove executes a move operation

// Parse source file

// --- helpers ---

func executeMove() {}

func other() {
	// executeMove is called above; body narration is never visited.
}
`

func TestStrandedComment_FiresOnFreeFloatingResidue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ops.go")
	if err := os.WriteFile(path, []byte(freeFloatingFixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := strandedCommentRule{}.Run(LintContext{Root: dir, Files: []string{path}})
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want exactly 1 (the free-floating executeMove comment): %+v", len(issues), issues)
	}
	iss := issues[0]
	if !strings.Contains(iss.Message, `opens with "executeMove"`) || !strings.Contains(iss.Message, "free-floating") {
		t.Errorf("unexpected message: %s", iss.Message)
	}
	if iss.Severity != "warning" {
		t.Errorf("severity = %q, want warning", iss.Severity)
	}
}

func TestStrandedComment_DocCommentNotDoubleReported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "types.go")
	if err := os.WriteFile(path, []byte(strandedFixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := strandedCommentRule{}.Run(LintContext{Root: dir, Files: []string{path}})
	for _, iss := range issues {
		if strings.Contains(iss.Message, "free-floating") {
			t.Errorf("attached doc comment reported by the free-floating path: %s", iss.Message)
		}
	}
}
