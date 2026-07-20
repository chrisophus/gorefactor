package analyzer

import "testing"

// True positives: an exported function and an exported type referenced only
// from a _test.go file are both flagged, tagged with their kind.
func TestTestOnlyLive_FlagsFuncAndType(t *testing.T) {
	got := testOnlyNames(t, map[string]string{
		"prod.go": `package p
func Helper() string { return "h" }
func Used() string { return "u" }
func Orphan() string { return "o" }
type Widget struct{ N int }
func consume() string { return Used() }
`,
		"prod_test.go": `package p
import "testing"
func TestX(t *testing.T) {
	_ = Helper()
	_ = Widget{N: 1}
}
`,
	})
	if got["Helper"] != "function" {
		t.Errorf("Helper used only in tests should be flagged as function, got %q", got["Helper"])
	}
	if got["Widget"] != "type" {
		t.Errorf("Widget used only in tests should be flagged as type, got %q", got["Widget"])
	}
	if _, ok := got["Used"]; ok {
		t.Error("Used is referenced in production; must not be flagged")
	}
	if _, ok := got["Orphan"]; ok {
		t.Error("Orphan is referenced nowhere (dead-code's job); must not be flagged test-only")
	}
}

// Negative: an unexported symbol used only in tests is not the rule's concern
// (the rule targets the exported API surface).
func TestTestOnlyLive_IgnoresUnexported(t *testing.T) {
	got := testOnlyNames(t, map[string]string{
		"prod.go": `package p
func helper() string { return "h" }
`,
		"prod_test.go": `package p
import "testing"
func TestX(t *testing.T) { _ = helper() }
`,
	})
	if _, ok := got["helper"]; ok {
		t.Error("unexported helper must not be flagged")
	}
}

// Negative: with no test files there is nothing to be test-only against.
func TestTestOnlyLive_NoTestFilesNoFindings(t *testing.T) {
	got := testOnlyNames(t, map[string]string{
		"prod.go": `package p
func Helper() string { return "h" }
`,
	})
	if len(got) != 0 {
		t.Errorf("expected no findings without test files, got %v", got)
	}
}

func testOnlyNames(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	paths := writeTempPkg(t, files)
	got := map[string]string{}
	for _, s := range DetectTestOnlyLiveSymbols(paths) {
		got[s.Name] = s.Kind
	}
	return got
}
