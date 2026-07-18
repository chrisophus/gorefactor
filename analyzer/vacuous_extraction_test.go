package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A whole-body block must not be proposed: the helper would trip the same
// rule the extraction is meant to fix, so the parent would become a delegate
// and the finding would merely move.
func TestLengthReductionSkipsWholeBodyBlock(t *testing.T) {
	path := writeVacuousModule(t, wholeBodySwitchSrc())
	res, err := RecommendLengthReduction(path, "Dispatch", DefaultLongFunctionLines)
	if err != nil {
		t.Fatal(err)
	}
	if res.Lines <= DefaultLongFunctionLines {
		t.Fatalf("test fixture must exceed the threshold, got %d lines", res.Lines)
	}
	if len(res.Extractions) != 0 {
		t.Fatalf("whole-body block must be skipped as vacuous, got %d extraction(s): %+v", len(res.Extractions), res.Extractions)
	}
}

func TestComplexityReductionSkipsWholeBodyBlock(t *testing.T) {
	path := writeVacuousModule(t, wholeBodySwitchSrc())
	res, err := RecommendComplexityReduction(path, "Dispatch", 15)
	if err != nil {
		t.Fatal(err)
	}
	if res.Complexity <= 15 {
		t.Fatalf("test fixture must exceed the threshold, got complexity %d", res.Complexity)
	}
	if len(res.Extractions) != 0 {
		t.Fatalf("whole-body block must be skipped as vacuous, got %d extraction(s): %+v", len(res.Extractions), res.Extractions)
	}
}

// A block whose helper stays under the budget must still be proposed — the
// guard rejects vacuous moves, not extraction itself.
func TestLengthReductionStillProposesModestBlocks(t *testing.T) {
	var b strings.Builder
	b.WriteString("package p\n\nfunc Long(xs []int) int {\n\ttotal := 0\n")
	// Two independent 40-line loops: each is a legitimate < 75-line helper.
	for block := 0; block < 2; block++ {
		b.WriteString("\tfor _, x := range xs {\n")
		for i := 0; i < 38; i++ {
			b.WriteString("\t\ttotal += x + ")
			b.WriteString(itoa(block*100 + i))
			b.WriteString("\n")
		}
		b.WriteString("\t}\n")
	}
	b.WriteString("\treturn total\n}\n")
	path := writeVacuousModule(t, b.String())
	res, err := RecommendLengthReduction(path, "Long", DefaultLongFunctionLines)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Extractions) == 0 {
		t.Fatalf("modest sub-blocks must still be proposed, got none (function is %d lines)", res.Lines)
	}
	for _, e := range res.Extractions {
		if span := e.EndLine - e.StartLine + 1; span+2 >= DefaultLongFunctionLines {
			t.Errorf("proposed helper would be %d lines, over the budget", span+2)
		}
	}
}

// writeVacuousModule writes src to a temp .go file and returns its path.
func writeVacuousModule(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// wholeBodySwitchSrc mimics the dispatch-table shape that once produced a
// vacuous autofix: the entire body is one giant switch, so the only
// extractable top-level block IS the body. Extracting it just renames the
// finding.
func wholeBodySwitchSrc() string {
	var b strings.Builder
	b.WriteString("package p\n\nfunc Dispatch(k int) int {\n\tswitch k {\n")
	for i := 0; i < 25; i++ {
		b.WriteString("\tcase ")
		b.WriteString(itoa(i))
		b.WriteString(":\n\t\tif k > ")
		b.WriteString(itoa(i * 2))
		b.WriteString(" {\n\t\t\treturn ")
		b.WriteString(itoa(i))
		b.WriteString("\n\t\t}\n\t\treturn ")
		b.WriteString(itoa(i + 100))
		b.WriteString("\n")
	}
	b.WriteString("\t}\n\treturn -1\n}\n")
	return b.String()

}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
