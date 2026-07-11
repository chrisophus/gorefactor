package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/chrisophus/gorefactor/analyzer"
)

func TestParseComplexityAutoFixCmd(t *testing.T) {
	file, fn, ok := parseComplexityAutoFixCmd("gorefactor recommend --reduce-complexity a/b.go Foo --apply")
	if !ok || file != "a/b.go" || fn != "Foo" {
		t.Fatalf("parse = (%q, %q, %v), want (a/b.go, Foo, true)", file, fn, ok)
	}
	if _, _, ok := parseComplexityAutoFixCmd("gorefactor split x.go"); ok {
		t.Fatalf("expected parse to fail on unrelated command")
	}
}

// writeComplexityModule drops a single-file module in a temp dir and returns the
// file path.
func writeComplexityModule(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module cxmod\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "big.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestReduceComplexityByExtraction_ExtractsPureBlock verifies the autofix lifts
// an over-threshold function's highest-contribution non-return block and that
// the result still parses.
func TestReduceComplexityByExtraction_ExtractsPureBlock(t *testing.T) {
	const src = `package cxmod

func Big(xs []int) int {
	total := 0
	for _, x := range xs {
		if x > 0 {
			total += x
		}
		if x < 0 {
			total -= x
		}
		if x == 0 {
			total++
		}
		if x > 100 {
			total += 100
		}
		if x > 1000 {
			total += 1000
		}
		if x%2 == 0 {
			total *= 2
		}
		if x%3 == 0 {
			total *= 3
		}
		if x%5 == 0 {
			total *= 5
		}
	}
	if total > 10 {
		total = 10
	}
	if total > 20 {
		total = 20
	}
	if total > 30 {
		total = 30
	}
	if total > 40 {
		total = 40
	}
	if total > 50 {
		total = 50
	}
	if total > 60 {
		total = 60
	}
	if total > 70 {
		total = 70
	}
	if total > 80 {
		total = 80
	}
	return total
}
`
	path := writeComplexityModule(t, src)

	applied, err := reduceComplexityByExtraction(path, "Big", defaultComplexityThreshold, false)
	if err != nil {
		t.Fatalf("reduceComplexityByExtraction: %v", err)
	}
	if applied < 1 {
		t.Fatalf("applied = %d, want >= 1 (the pure for-loop block is extractable)", applied)
	}

	// The rewritten file must still parse.
	if _, err := parser.ParseFile(token.NewFileSet(), path, nil, 0); err != nil {
		t.Fatalf("rewritten file does not parse: %v", err)
	}
	// And Big's complexity must have dropped.
	red, err := analyzer.RecommendComplexityReduction(path, "Big", defaultComplexityThreshold)
	if err != nil {
		t.Fatalf("re-analyze: %v", err)
	}
	if red.Complexity >= defaultComplexityThreshold*2 {
		t.Errorf("complexity after extraction = %d, expected a meaningful reduction", red.Complexity)
	}
}

// TestReduceComplexityByExtraction_ReturnHeavyIsBestEffort verifies that a
// function whose complexity lives entirely in return-bearing branches yields
// zero applied extractions (the extract engine refuses return blocks) rather
// than an error or a mangled file.
func TestReduceComplexityByExtraction_ReturnHeavyIsBestEffort(t *testing.T) {
	const src = `package cxmod

import "errors"

func Guard(n int) (int, error) {
	if n == 1 {
		return 0, errors.New("1")
	}
	if n == 2 {
		return 0, errors.New("2")
	}
	if n == 3 {
		return 0, errors.New("3")
	}
	if n == 4 {
		return 0, errors.New("4")
	}
	if n == 5 {
		return 0, errors.New("5")
	}
	if n == 6 {
		return 0, errors.New("6")
	}
	if n == 7 {
		return 0, errors.New("7")
	}
	if n == 8 {
		return 0, errors.New("8")
	}
	if n == 9 {
		return 0, errors.New("9")
	}
	if n == 10 {
		return 0, errors.New("10")
	}
	if n == 11 {
		return 0, errors.New("11")
	}
	if n == 12 {
		return 0, errors.New("12")
	}
	if n == 13 {
		return 0, errors.New("13")
	}
	if n == 14 {
		return 0, errors.New("14")
	}
	if n == 15 {
		return 0, errors.New("15")
	}
	if n == 16 {
		return 0, errors.New("16")
	}
	return n, nil
}
`
	path := writeComplexityModule(t, src)
	before, _ := os.ReadFile(path)

	applied, err := reduceComplexityByExtraction(path, "Guard", defaultComplexityThreshold, false)
	if err != nil {
		t.Fatalf("reduceComplexityByExtraction: %v", err)
	}
	if applied != 0 {
		t.Fatalf("applied = %d, want 0 (every block returns)", applied)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Errorf("file was mutated despite zero successful extractions")
	}
}
