package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The reducer resolves Receiver:Method locators and greedily picks the big
// for-block, projecting the parent under the threshold.
func TestRecommendLengthReduction_MethodLocator(t *testing.T) {
	path := writeLengthFixture(t)
	res, err := RecommendLengthReduction(path, "S:Long", 75)
	if err != nil {
		t.Fatalf("RecommendLengthReduction: %v", err)
	}
	if len(res.Extractions) == 0 {
		t.Fatalf("expected extractions for an %d-line method", res.Lines)
	}
	if res.Projected > 75 {
		t.Errorf("projected = %d, want <= 75", res.Projected)
	}
	if res.Extractions[0].Suggestion == "" {
		t.Error("extraction has no suggested helper name")
	}
}

// A function already under the threshold yields no extractions, and an
// unknown locator errors.
func TestRecommendLengthReduction_UnderThresholdAndMissing(t *testing.T) {
	path := writeLengthFixture(t)
	res, err := RecommendLengthReduction(path, "Short", 75)
	if err != nil {
		t.Fatalf("RecommendLengthReduction: %v", err)
	}
	if len(res.Extractions) != 0 {
		t.Errorf("extractions = %d, want 0 for a short function", len(res.Extractions))
	}
	if _, err := RecommendLengthReduction(path, "Nope", 75); err == nil {
		t.Error("expected an error for a missing function")
	}
}

func writeLengthFixture(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("package p\n\ntype S struct{}\n\nfunc (s S) Long(xs []int) int {\n\ttotal := 0\n\tfor _, x := range xs {\n")
	for i := 0; i < 40; i++ {
		b.WriteString("\t\ttotal += x\n")
	}
	b.WriteString("\t}\n")
	for i := 0; i < 40; i++ {
		b.WriteString("\ttotal++\n")
	}
	b.WriteString("\treturn total\n}\n\nfunc Short() {}\n")
	path := filepath.Join(t.TempDir(), "p.go")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
