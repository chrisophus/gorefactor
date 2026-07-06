package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsIdiomaticErrorBlock(t *testing.T) {
	excluded := []string{
		"if err != nil { return err }",
		"if err != nil {\n\treturn nil, err\n}",
		"if err != nil { return 0, err }",
		"if err != nil { return false, err }",
	}
	for _, c := range excluded {
		if !isIdiomaticErrorBlock(NormalizeCode(c)) {
			t.Errorf("expected excluded: %q", c)
		}
	}
	if isIdiomaticErrorBlock(NormalizeCode("x := 1\ny := 2\nz := x + y")) {
		t.Error("a real 3-statement block must not be excluded")
	}
}

func TestDuplicateIgnorePatterns(t *testing.T) {
	old := DuplicateIgnorePatterns
	defer func() { DuplicateIgnorePatterns = old }()
	DuplicateIgnorePatterns = []string{"t.Fatal"}
	if !isIdiomaticErrorBlock(NormalizeCode("t.Fatal(err)")) {
		t.Error("configured ignore pattern should exclude the block")
	}
}

func TestRecommendComplexityReduction(t *testing.T) {
	src := `package p

func Big(items []int) int {
	total := 0
	for _, x := range items {
		if x > 0 {
			total += x
		}
	}
	if total > 100 {
		total = 100
	}
	switch total {
	case 0:
		total = -1
	}
	return total
}
`
	dir := t.TempDir()
	file := filepath.Join(dir, "p.go")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := RecommendComplexityReduction(file, "Big", 2)
	if err != nil {
		t.Fatalf("RecommendComplexityReduction: %v", err)
	}
	if res.Complexity <= res.Threshold {
		t.Fatalf("expected over-threshold, got complexity %d", res.Complexity)
	}
	if len(res.Extractions) == 0 {
		t.Fatal("expected at least one extraction suggestion")
	}
	if res.Projected >= res.Complexity {
		t.Fatalf("projected (%d) should be below original (%d)", res.Projected, res.Complexity)
	}
	// Highest-contribution block should be suggested first.
	for i := 1; i < len(res.Extractions); i++ {
		if res.Extractions[i-1].Complexity < res.Extractions[i].Complexity {
			t.Error("extractions not sorted by complexity contribution (desc)")
		}
	}
}

func TestRecommendComplexityReduction_UnderThreshold(t *testing.T) {
	src := "package p\n\nfunc Small() int { return 1 }\n"
	dir := t.TempDir()
	file := filepath.Join(dir, "p.go")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := RecommendComplexityReduction(file, "Small", 15)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Extractions) != 0 {
		t.Fatalf("under-threshold function should yield no extractions, got %d", len(res.Extractions))
	}
}
