package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The residue rules exist because both defects shipped in this repo:
// extractBlockL* names from a bulk autofix, and a by-value bytes.Buffer
// parameter that silently emptied generated test scaffolds.
func TestResidueRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	src := `package p

import (
	"bytes"
	"fmt"
	"strings"
)

func extractBlockL42() {}

func extractBlockLX() {} // not a positional fallback: no digits

func lostWrites(buf bytes.Buffer, s string) {
	fmt.Fprintf(&buf, "%s", s)
}

func lostBuilder(b strings.Builder) {
	b.WriteString("x")
}

func fine(buf *bytes.Buffer, b *strings.Builder) {
	buf.WriteString("x")
	b.WriteString("y")
}
`
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := LintContext{Root: dir, Files: []string{path}}

	gn := generatedNameRule{}.Run(ctx)
	if len(gn) != 1 {
		t.Fatalf("generated-name findings = %d, want exactly 1 (extractBlockL42): %+v", len(gn), gn)
	}

	bb := byValueBufferRule{}.Run(ctx)
	if len(bb) != 2 {
		t.Fatalf("byvalue-buffer findings = %d, want 2 (Buffer + Builder by value): %+v", len(bb), bb)
	}
	for _, iss := range bb {
		if iss.Severity != "warning" {
			t.Errorf("byvalue-buffer severity = %q, want warning", iss.Severity)
		}
	}
}
