package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAdviseRename_BlockingHints pins the definite-problem cases: these are
// the only conditions under which the advisor blocks, because they hold
// regardless of scope resolution.
func TestAdviseRename_BlockingHints(t *testing.T) {
	advisor, err := NewRenameAdvisor(writeAdvisorFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		old, new string
		wantHint string
	}{
		{"invalid identifier", "Target", "bad-name", "invalid identifier"},
		{"builtin collision", "Target", "len", "conflicts with builtin"},
		{"package-level collision", "Target", "Existing", "already declared at package level"},
		{"missing symbol", "NoSuchSymbol", "Whatever", "symbol not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hints, err := advisor.AdviseRename(tc.old, tc.new)
			if err != nil {
				t.Fatal(err)
			}
			if !hints.HasBlocking() {
				t.Fatalf("expected blocking hints, got none: %+v", hints)
			}
			if !strings.Contains(strings.Join(hints.Blocking, "\n"), tc.wantHint) {
				t.Errorf("blocking hints %v do not mention %q", hints.Blocking, tc.wantHint)
			}
		})
	}
}

// TestAdviseRename_AdvisoryNotVerdict is harness-integrity plan item 7's
// acceptance: for a plausible rename the advisor produces advisory hints
// stating its name-match-only limits — never a safety verdict.
func TestAdviseRename_AdvisoryNotVerdict(t *testing.T) {
	advisor, err := NewRenameAdvisor(writeAdvisorFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	hints, err := advisor.AdviseRename("Target", "Renamed")
	if err != nil {
		t.Fatal(err)
	}
	if hints.HasBlocking() {
		t.Fatalf("unexpected blocking hints: %v", hints.Blocking)
	}
	joined := strings.Join(hints.Advisory, "\n")
	if !strings.Contains(joined, "exported") || !strings.Contains(joined, "name-match") {
		t.Errorf("advisory hints must state the exported-symbol and name-match-only caveats, got: %v", hints.Advisory)
	}
	report := hints.Report("Target", "Renamed")
	if !strings.Contains(report, "name-match only") {
		t.Errorf("report must state the name-match-only limit, got:\n%s", report)
	}
	// The old surface printed a "Status: true/false" verdict line; the
	// advisory report must not render any verdict.
	if strings.Contains(report, "Status:") || strings.Contains(report, "SafeToRename") {
		t.Errorf("report must not render a safety verdict, got:\n%s", report)
	}
}

func writeAdvisorFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := `package fixture

// Target is the symbol under rename.
func Target() int { return helper() }

func helper() int { return 1 }

var Existing = 2
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
