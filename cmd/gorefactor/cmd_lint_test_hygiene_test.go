package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const hygieneFixture = `package p

import (
	"testing"
	"time"
)

func TestVacuous(t *testing.T) {
	result := 1 + 1
	_ = result
}

func TestAsserts(t *testing.T) {
	if 1+1 != 2 {
		t.Fatal("math is broken")
	}
}

func TestUsesHelper(t *testing.T) {
	mustBeTwo(t, 1+1)
}

func TestSubtest(t *testing.T) {
	t.Run("inner", func(t *testing.T) {
		t.Error("boom")
	})
}

func TestSleeps(t *testing.T) {
	time.Sleep(50 * time.Millisecond)
	if 1+1 != 2 {
		t.Fatal("nope")
	}
}

func TestMain(m *testing.M) {}

func mustBeTwo(t *testing.T, n int) {
	if n != 2 {
		t.Fatalf("want 2, got %d", n)
	}
}
`

func TestVacuousTestRule(t *testing.T) {
	issues := vacuousTestRule{}.Run(writeHygieneFixture(t))
	if len(issues) != 1 {
		t.Fatalf("want exactly 1 vacuous test, got %+v", issues)
	}
	if !strings.Contains(issues[0].Message, "TestVacuous") {
		t.Fatalf("wrong test flagged: %s", issues[0].Message)
	}
}

func TestSleepInTestRule(t *testing.T) {
	issues := sleepInTestRule{}.Run(writeHygieneFixture(t))
	if len(issues) != 1 || !strings.Contains(issues[0].File, "hygiene_test.go") {
		t.Fatalf("want exactly 1 sleep finding, got %+v", issues)
	}
}

func TestHygieneRulesSkipNonTestFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prod.go")
	src := "package p\n\nimport \"time\"\n\nfunc wait() { time.Sleep(time.Second) }\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := LintContext{Root: dir, Files: []string{path}}
	sleepIssues := sleepInTestRule{}.Run(ctx)
	if len(sleepIssues) != 0 {
		t.Fatalf("sleep in production code is out of scope: %+v", sleepIssues)
	}
	vacuousIssues := vacuousTestRule{}.Run(ctx)
	if len(vacuousIssues) != 0 {
		t.Fatalf("non-test files are out of scope: %+v", vacuousIssues)
	}
}

func writeHygieneFixture(t *testing.T) LintContext {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "hygiene_test.go")
	if err := os.WriteFile(path, []byte(hygieneFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	return LintContext{Root: dir, Files: []string{path}}
}
