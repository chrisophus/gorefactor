package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseLintOptions_FixLevel covers the aggressive knob's contract:
// aggressive demands --fix --verify, unknown levels are rejected, and the
// default stays safe.
func TestParseLintOptions_FixLevel(t *testing.T) {
	if opts, err := parseLintOptions(nil); err != nil || opts.fixLevel != fixLevelSafe {
		t.Fatalf("default fixLevel = %q, err = %v; want safe, nil", opts.fixLevel, err)
	}
	if _, err := parseLintOptions([]string{"--fix-level", "aggressive"}); err == nil {
		t.Fatal("aggressive without --fix --verify must be rejected")
	}
	if _, err := parseLintOptions([]string{"--fix", "--fix-level", "aggressive"}); err == nil {
		t.Fatal("aggressive without --verify must be rejected")
	}
	opts, err := parseLintOptions([]string{"--fix", "--verify", "--fix-level", "aggressive"})
	if err != nil {
		t.Fatalf("aggressive with --fix --verify: %v", err)
	}
	if !opts.lintContext(nil).AggressiveFix() {
		t.Fatal("LintContext.AggressiveFix() = false, want true")
	}
	if _, err := parseLintOptions([]string{"--fix", "--verify", "--fix-level", "bogus"}); err == nil {
		t.Fatal("unknown fix level must be rejected")
	}
}

// writeLongFunctionModule builds a module whose single function is over the
// long-function threshold with one large self-contained block.
func writeLongFunctionModule(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("package lfmod\n\nfunc Big(xs []int) int {\n\ttotal := 0\n")
	// A ~40-line self-contained block: declares its own accumulator and
	// folds it into total via a single trailing assignment... kept simple:
	// one big for block that only touches loop-locals and a slice it owns.
	b.WriteString("\tout := make([]int, 0, len(xs))\n\tfor _, x := range xs {\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "\t\tout = append(out, x+%d)\n", i)
	}
	b.WriteString("\t}\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "\ttotal += %d\n", i)
	}
	b.WriteString("\ttotal += len(out)\n\treturn total\n}\n")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module lfmod\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "big.go")
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(b.String(), " ", " ")), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The long-function rule attaches its autofix only at the aggressive level.
func TestLongFunctionRule_AutoFixIsAggressiveOnly(t *testing.T) {
	path := writeLongFunctionModule(t)
	safe := (longFunctionRule{}).Run(LintContext{Files: []string{path}})
	if len(safe) == 0 {
		t.Fatal("expected a long-function issue")
	}
	for _, iss := range safe {
		if iss.AutoFixCmd != "" {
			t.Errorf("safe level attached AutoFixCmd %q", iss.AutoFixCmd)
		}
	}
	agg := (longFunctionRule{}).Run(LintContext{Files: []string{path}, FixLevel: fixLevelAggressive})
	if len(agg) == 0 {
		t.Fatal("expected a long-function issue at aggressive level")
	}
	found := false
	for _, iss := range agg {
		if strings.Contains(iss.AutoFixCmd, "--reduce-length") && strings.Contains(iss.AutoFixCmd, "--allow-returns") {
			found = true
		}
	}
	if !found {
		t.Errorf("aggressive level did not attach a reduce-length autofix: %+v", agg)
	}
}

// End to end: the attached autofix actually shortens the function under the
// threshold and leaves a parseable file.
func TestLongFunctionRule_AutoFixShortens(t *testing.T) {
	path := writeLongFunctionModule(t)
	ctx := LintContext{Files: []string{path}, FixLevel: fixLevelAggressive}
	issues := (longFunctionRule{}).Run(ctx)
	if len(issues) == 0 || issues[0].AutoFixCmd == "" {
		t.Fatalf("expected an autofixable issue, got %+v", issues)
	}
	if err := (longFunctionRule{}).AutoFix(issues[0], ctx); err != nil {
		t.Fatalf("AutoFix: %v", err)
	}
	mustParse(t, path)
	if again := (longFunctionRule{}).Run(LintContext{Files: []string{path}}); len(again) != 0 {
		t.Errorf("function still over threshold after autofix: %+v", again)
	}
}

func TestParseReduceLengthAutoFixCmd(t *testing.T) {
	file, fn, ok := parseReduceLengthAutoFixCmd("gorefactor recommend --reduce-length a/b.go Foo --max-lines 75 --apply --allow-returns")
	if !ok || file != "a/b.go" || fn != "Foo" {
		t.Fatalf("parse = (%q, %q, %v), want (a/b.go, Foo, true)", file, fn, ok)
	}
	if _, _, ok := parseReduceLengthAutoFixCmd("gorefactor split x.go"); ok {
		t.Fatal("expected parse to fail on unrelated command")
	}
}

// The dead-code rule reports module-unreferenced exported functions only at
// the aggressive level, and never touches exported methods.
func TestDeadCodeRule_AggressiveExportedFunctions(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := write("a.go", `package p

// OrphanExported is referenced nowhere in the module.
func OrphanExported() int { return 1 }

// UsedExported is called from b.go.
func UsedExported() int { return 2 }

type T struct{}

// String is exported but a method: reflection/interface dispatch makes
// deleting it unsafe even aggressively.
func (T) String() string { return "t" }
`)
	b := write("b.go", `package p

var _ = UsedExported()
`)
	files := []string{a, b}

	safeIssues := checkDeadCode(LintContext{Files: files})
	for _, iss := range safeIssues {
		if strings.Contains(iss.Message, "OrphanExported") {
			t.Errorf("safe level flagged exported function: %s", iss.Message)
		}
	}

	aggIssues := checkDeadCode(LintContext{Files: files, FixLevel: fixLevelAggressive})
	var orphan, used, method bool
	for _, iss := range aggIssues {
		if strings.Contains(iss.Message, "OrphanExported") {
			orphan = true
			if want := "delete " + a + " OrphanExported --safe"; iss.AutoFixCmd != want {
				t.Errorf("AutoFixCmd = %q, want %q", iss.AutoFixCmd, want)
			}
		}
		if strings.Contains(iss.Message, "UsedExported") {
			used = true
		}
		if strings.Contains(iss.Message, "String") {
			method = true
		}
	}
	if !orphan {
		t.Error("aggressive level did not flag OrphanExported")
	}
	if used {
		t.Error("aggressive level flagged UsedExported, which b.go references")
	}
	if method {
		t.Error("aggressive level flagged an exported method")
	}
}
