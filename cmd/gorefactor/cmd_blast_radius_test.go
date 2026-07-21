package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chrisophus/gorefactor/analyzer"
)

func TestBlastRadiusCommandJSON(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", callgraphTestSrc)

	out := captureStdout(t, func() {
		if err := blastRadiusCommand([]string{"Leaf", "--json"}); err != nil {
			t.Fatalf("blast-radius: %v", err)
		}
	})

	var br blastRadius
	decodeEnvelope(t, out, &br)
	if br.Target != "Leaf" {
		t.Fatalf("target = %q, want Leaf", br.Target)
	}
	// Leaf is called by Middle; Middle by Top and Loop; Top by Svc:Handle.
	// Transitive closure = {Middle, Top, Loop, Svc:Handle} = 4.
	if br.DirectCallers != 1 {
		t.Errorf("directCallers = %d, want 1", br.DirectCallers)
	}
	if br.TransitiveCallers != 4 {
		t.Errorf("transitiveCallers = %d, want 4", br.TransitiveCallers)
	}
	if !br.Exported {
		t.Errorf("Leaf should be reported as exported")
	}
	// score = 4*2 + 1 file + 1 pkg + 5 (exported) = 15 -> medium.
	if br.Score != 15 {
		t.Errorf("score = %d, want 15", br.Score)
	}
	if br.Level != "medium" {
		t.Errorf("level = %q, want medium", br.Level)
	}
}

func TestBlastRadiusCommandNotFound(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", callgraphTestSrc)

	err := blastRadiusCommand([]string{"Nope"})
	assertExitCode(t, err, exitNotFound)
}

func TestBlastRadiusRuleFlagsLoadBearing(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("package x\n\nfunc Hub() {}\n\nfunc Lonely() {}\n")
	for i := 0; i < highBlastRadiusTransitive+5; i++ {
		fmt.Fprintf(&b, "func C%d() { Hub() }\n", i)
	}
	writeTempGo(t, dir, "x.go", b.String())

	files, err := collectGoFiles(dir, analyzer.DefaultWalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	issues := blastRadiusRule{}.Run(LintContext{Root: dir, Files: files})

	hubFlagged := false
	for _, iss := range issues {
		if iss.Rule != "high-blast-radius" {
			t.Errorf("Rule = %q, want high-blast-radius", iss.Rule)
		}
		if iss.Severity != "info" {
			t.Errorf("Severity = %q, want info", iss.Severity)
		}
		if strings.HasPrefix(iss.Message, "Hub ") {
			hubFlagged = true
		}
		if strings.HasPrefix(iss.Message, "Lonely ") {
			t.Errorf("Lonely (no callers) should not be flagged: %s", iss.Message)
		}
	}
	if !hubFlagged {
		t.Errorf("Hub should be flagged as load-bearing; got issues: %v", issues)
	}
}
