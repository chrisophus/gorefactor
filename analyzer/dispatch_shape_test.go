package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// A flat dispatcher whose total complexity exceeds the threshold must
// normalize to a small per-branch score.
func TestAnalyzeDispatchFlatWalkerNormalizesLow(t *testing.T) {
	_, d, total := parseFirstFunc(t, flatWalkerSrc(16))
	if total <= 15 {
		t.Fatalf("fixture must exceed the threshold, total complexity %d", total)
	}
	if d == nil {
		t.Fatal("dispatch shape not detected")
	}
	if d.Cases != 16 {
		t.Fatalf("cases = %d, want 16", d.Cases)
	}
	if d.NormalizedComplexity > 15 {
		t.Fatalf("per-branch score %d must be under threshold (worst case %d)", d.NormalizedComplexity, d.WorstCaseComplexity)
	}
}

// One huge case means the table shape does not excuse the finding: the
// worst branch dominates the normalized score.
func TestAnalyzeDispatchHugeCaseStaysHigh(t *testing.T) {
	var b strings.Builder
	b.WriteString("package p\n\nfunc lop(k, x int) int {\n\tswitch k {\n\tcase 1:\n\t\treturn 1\n\tcase 2:\n")
	for i := 0; i < 20; i++ {
		b.WriteString("\t\tif x > ")
		b.WriteString(itoaTest(i))
		b.WriteString(" {\n\t\t\tx++\n\t\t}\n")
	}
	b.WriteString("\t\treturn x\n\t}\n\treturn 0\n}\n")
	_, d, total := parseFirstFunc(t, b.String())
	if total <= 15 {
		t.Fatalf("fixture must exceed the threshold, total complexity %d", total)
	}
	if d == nil {
		t.Fatal("dispatch shape not detected")
	}
	if d.NormalizedComplexity <= 15 {
		t.Fatalf("per-branch score %d must stay over threshold when one case holds the bulk", d.NormalizedComplexity)
	}
}

// fallthrough makes cases order-dependent — not a table.
func TestAnalyzeDispatchFallthroughIneligible(t *testing.T) {
	src := `package p

func ft(k int) int {
	switch k {
	case 1:
		k++
		fallthrough
	case 2:
		return k
	}
	return 0
}
`
	_, d, _ := parseFirstFunc(t, src)
	if d != nil {
		t.Fatalf("fallthrough switch must not be treated as a dispatch table, got %+v", d)
	}
}

// A tangle with no top-level switch has no dispatch shape at all.
func TestAnalyzeDispatchNoSwitchIsNil(t *testing.T) {
	src := `package p

func tangle(xs []int) int {
	total := 0
	for _, x := range xs {
		if x > 0 {
			total += x
		}
	}
	return total
}
`
	_, d, _ := parseFirstFunc(t, src)
	if d != nil {
		t.Fatalf("expected nil dispatch info, got %+v", d)
	}
}

// A multi-line string literal counts as data, not logic: a function that
// returns an 80-line prompt template has single-digit logic lines.
func TestLogicLinesExcludesMultilineStrings(t *testing.T) {
	var b strings.Builder
	b.WriteString("package p\n\nfunc prompt() string {\n\treturn `line\n")
	for i := 0; i < 80; i++ {
		b.WriteString("text line\n")
	}
	b.WriteString("`\n}\n")
	ms, err := FunctionMetricsForSource("f.go", []byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("want 1 function, got %d", len(ms))
	}
	if ms[0].Lines < 80 {
		t.Fatalf("fixture must be long, got %d lines", ms[0].Lines)
	}
	if got := ms[0].LogicLines(); got > 5 {
		t.Fatalf("LogicLines = %d, want <= 5 (string literal is data)", got)
	}
}

// parseFirstFunc parses src and returns the fset and the first function decl.
func parseFirstFunc(t *testing.T, src string) (*token.FileSet, *DispatchInfo, int) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "f.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			return fset, AnalyzeDispatch(fset, fn), calculateFunctionComplexity(fn)
		}
	}
	t.Fatal("no function in fixture")
	return nil, nil, 0
}

// flatWalkerSrc builds an AST-walker-shaped type switch: many independent
// cases, each with a small guard — genuinely branchy in total, trivially
// readable per case.
func flatWalkerSrc(cases int) string {
	var b strings.Builder
	b.WriteString("package p\n\nfunc walk(n interface{}) int {\n\tswitch v := n.(type) {\n")
	for i := 0; i < cases; i++ {
		b.WriteString("\tcase [")
		b.WriteString(itoaTest(i + 1))
		b.WriteString("]int:\n\t\tif v[0] > 0 {\n\t\t\treturn v[0]\n\t\t}\n\t\treturn -1\n")
	}
	b.WriteString("\t}\n\treturn 0\n}\n")
	return b.String()
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}
