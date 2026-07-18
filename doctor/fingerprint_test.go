package doctor

import "testing"

func TestNormalizeMessageCollapsesDigitRuns(t *testing.T) {
	got := NormalizeMessage("is 80 lines (threshold 75, line 98)")
	want := "is # lines (threshold #, line #)"
	if got != want {
		t.Fatalf("normalizeMessage: got %q want %q", got, want)
	}
}

func TestFingerprintLineIndependent(t *testing.T) {
	a := Finding{File: "a.go", Rule: "r", Message: "block at lines 10-20 duplicated"}
	b := Finding{File: "a.go", Rule: "r", Message: "block at lines 315-325 duplicated"}
	if fingerprint(a) != fingerprint(b) {
		t.Fatal("fingerprints must be line-number independent")
	}
	c := Finding{File: "b.go", Rule: "r", Message: a.Message}
	if fingerprint(a) == fingerprint(c) {
		t.Fatal("different files must fingerprint differently")
	}
}

func TestCategoryDefaultSeverity(t *testing.T) {
	for _, c := range []Category{CategoryConc, CategorySec, CategoryAPI, CategoryTemporal} {
		if c.DefaultSeverity() != SeverityError {
			t.Fatalf("%s should derive error severity", c)
		}
	}
	for _, c := range []Category{CategoryPerf, CategoryDead, CategoryStruct, CategoryLint} {
		if c.DefaultSeverity() != SeverityWarning {
			t.Fatalf("%s should derive warning severity", c)
		}
	}
}
