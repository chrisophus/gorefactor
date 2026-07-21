package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUntestedFunctionRule_NoProfile(t *testing.T) {
	dir := t.TempDir()
	if got := (untestedFunctionRule{}).Run(LintContext{Root: dir}); got != nil {
		t.Fatalf("expected no findings without coverage.out, got %d", len(got))
	}
}

func TestUntestedFunctionRule_StaleProfile(t *testing.T) {
	dir := t.TempDir()
	// A syntactically valid profile referencing a file that does not exist:
	// `go tool cover -func` exits non-zero, which must surface as a finding,
	// not silently disable the rule.
	profile := "mode: set\ngithub.com/nosuch/mod/deleted_file.go:1.1,2.2 1 0\n"
	if err := os.WriteFile(filepath.Join(dir, "coverage.out"), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}
	got := (untestedFunctionRule{}).Run(LintContext{Root: dir})
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 stale-profile finding, got %d", len(got))
	}
	if got[0].Rule != "untested-function" || got[0].Severity != "info" {
		t.Fatalf("unexpected finding shape: %+v", got[0])
	}
	if !strings.Contains(got[0].Message, "stale or unreadable") {
		t.Fatalf("message should name the stale profile, got %q", got[0].Message)
	}
}

func TestExportedFuncName(t *testing.T) {
	cases := []struct {
		raw      string
		name     string
		exported bool
	}{
		{"Func", "Func", true},
		{"pkg.Func", "Func", true},
		{"pkg.helper", "helper", false},
		{"(Recv).Method", "Method", true},
		{"(*Recv).Method", "Method", true},
		{"(*Recv).method", "method", false},
	}
	for _, c := range cases {
		name, exported := exportedFuncName(c.raw)
		if name != c.name || exported != c.exported {
			t.Errorf("exportedFuncName(%q) = (%q, %v), want (%q, %v)", c.raw, name, exported, c.name, c.exported)
		}
	}
}
