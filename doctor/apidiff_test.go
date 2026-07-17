package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPIDiffGatesUndeclaredChanges(t *testing.T) {
	root := gitRepo(t, map[string]string{"api.go": "package api\n\nfunc Sig(a int) {}\n\nfunc Gone() {}\n"})
	if err := os.WriteFile(filepath.Join(root, "api.go"),
		[]byte("package api\n\nfunc Sig(a int, b string) {}\n\nfunc Fresh() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := (APIDiff{}).Run(RunContext{Root: root, BaseRef: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	byRule := map[string]Finding{}
	for _, f := range findings {
		byRule[f.Rule] = f
	}
	if f := byRule["api-removed"]; f.Severity != SeverityError || !f.New || !strings.Contains(f.Message, "api.Gone") {
		t.Fatalf("removed symbol should be a new error: %+v", f)
	}
	if f := byRule["api-changed"]; f.Severity != SeverityError || !strings.Contains(f.Message, "api.Sig") {
		t.Fatalf("changed signature should be an error: %+v", f)
	}
	if f := byRule["api-added"]; f.Severity != SeverityWarning || !strings.Contains(f.Message, "api.Fresh") {
		t.Fatalf("addition should be a warning, never a gate: %+v", f)
	}
}

func TestAPIDiffHonorsDeclaredIntent(t *testing.T) {
	root := gitRepo(t, map[string]string{"api.go": "package api\n\nfunc Sig(a int) {}\n"})
	if err := os.WriteFile(filepath.Join(root, "api.go"),
		[]byte("package api\n\nfunc Sig(a int, b string) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddIntent(root, Intent{Type: IntentAPIChange, Scope: "api.Sig", Reason: "widening for tests"}); err != nil {
		t.Fatal(err)
	}
	findings, err := (APIDiff{}).Run(RunContext{Root: root, BaseRef: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding: %+v", findings)
	}
	f := findings[0]
	if f.Severity != SeverityInfo || !strings.Contains(f.Message, "widening for tests") {
		t.Fatalf("declared delta should demote to info citing the reason: %+v", f)
	}
}

func TestAPIDiffScopedIntentDoesNotBlanket(t *testing.T) {
	root := gitRepo(t, map[string]string{
		"a/a.go": "package a\n\nfunc InScope() {}\n",
		"b/b.go": "package b\n\nfunc OutOfScope() {}\n",
	})
	if err := os.WriteFile(filepath.Join(root, "a", "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b", "b.go"), []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddIntent(root, Intent{Type: IntentAPIChange, Scope: "a", Reason: "only a"}); err != nil {
		t.Fatal(err)
	}
	findings, err := (APIDiff{}).Run(RunContext{Root: root, BaseRef: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		declared := strings.Contains(f.Message, "declared:")
		switch {
		case strings.Contains(f.Message, "a.InScope") && (f.Severity != SeverityInfo || !declared):
			t.Fatalf("a.InScope is declared: %+v", f)
		case strings.Contains(f.Message, "b.OutOfScope") && (f.Severity != SeverityError || declared):
			t.Fatalf("b.OutOfScope must still gate: %+v", f)
		}
	}
}
