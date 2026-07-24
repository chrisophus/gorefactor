package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHardToMaintainRule_LongSimpleQuiet(t *testing.T) {
	path := writeHardToMaintainFixture(t, longSimpleOrchestratorSrc())
	issues := hardToMaintainRule{}.Run(LintContext{Files: []string{path}})
	for _, iss := range issues {
		if strings.Contains(iss.Message, "orchestrate") {
			t.Fatalf("long-but-simple orchestrator must not be flagged: %+v", iss)
		}
	}
}

func TestHardToMaintainRule_LongAndComplexFires(t *testing.T) {
	path := writeHardToMaintainFixture(t, longComplexSrc())
	issues := hardToMaintainRule{}.Run(LintContext{Files: []string{path}})
	if len(issues) == 0 {
		t.Fatal("expected hard-to-maintain on long+complex function")
	}
	if issues[0].Severity != "warning" && issues[0].Severity != "error" {
		t.Fatalf("severity = %q, want warning or error", issues[0].Severity)
	}
	if !strings.Contains(issues[0].Message, "complexity") {
		t.Fatalf("message should cite complexity: %s", issues[0].Message)
	}
}

func TestHardToMaintainRule_ShortComplexQuiet(t *testing.T) {
	path := writeHardToMaintainFixture(t, `package p
func shortComplex(xs []int) int {
	n := 0
	for _, x := range xs {
		if x > 0 {
			if x%2 == 0 {
				n += x
			} else if x%3 == 0 {
				n -= x
			} else {
				n++
			}
		}
	}
	return n
}
`)
	issues := hardToMaintainRule{}.Run(LintContext{Files: []string{path}})
	if len(issues) != 0 {
		t.Fatalf("short functions must not fire even if branchy: %+v", issues)
	}
}

func writeHardToMaintainFixture(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func longSimpleOrchestratorSrc() string {
	var b strings.Builder
	b.WriteString("package p\n\nfunc orchestrate(a, b, c, d int) int {\n")
	b.WriteString("\tx := a\n")
	for i := 0; i < 80; i++ {
		b.WriteString("\tx = x + 1\n")
	}
	b.WriteString("\treturn x + b + c + d\n}\n")
	return b.String()
}

func longComplexSrc() string {
	var b strings.Builder
	b.WriteString("package p\n\nfunc tangle(xs []int) int {\n\ttotal := 0\n")
	for i := 0; i < 20; i++ {
		b.WriteString("\tfor _, x := range xs {\n")
		b.WriteString("\t\tif x > ")
		b.WriteString(itoaFixture(i))
		b.WriteString(" {\n\t\t\tif x%2 == 0 {\n\t\t\t\ttotal += x\n\t\t\t} else {\n\t\t\t\ttotal -= x\n\t\t\t}\n\t\t}\n\t}\n")
	}
	b.WriteString("\treturn total\n}\n")
	return b.String()
}
