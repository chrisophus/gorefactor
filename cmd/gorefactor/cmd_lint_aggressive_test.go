package main

import (
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

// A ~40-line self-contained block: declares its own accumulator and
// folds it into total via a single trailing assignment... kept simple:
// one big for block that only touches loop-locals and a slice it owns.
