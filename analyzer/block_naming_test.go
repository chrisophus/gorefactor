package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestSuggestBlockName(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"leading comment", "// Parse the optional configuration flags\nfor i := 0; i < 3; i++ {\n_ = i\n}", "parseOptionalConfigurationFlags"},
		{"comment stopwords dropped", "// build a report\nx := 1\n_ = x", "buildReport"},
		{"assign fallback", "total := 1 + 2\n_ = total", "computeTotal"},
		{"range fallback", "for _, item := range items {\n_ = item\n}", "processItems"},
		{"keyword-safe", "// range\nx := 1\n_ = x", "rangeBlock"},
		{"positional fallback", "for i := 0; i < 3; i++ {\n_ = i\n}", "extractBlockL2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt, cmap := blockStmtFromSrc(t, tt.src)
			got := SuggestBlockName(stmt, cmap, 2, map[string]bool{})
			if got != tt.want {
				t.Errorf("SuggestBlockName = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSuggestBlockNameUnique(t *testing.T) {
	used := map[string]bool{}
	stmt, cmap := blockStmtFromSrc(t, "total := 1\n_ = total")
	if got := SuggestBlockName(stmt, cmap, 2, used); got != "computeTotal" {
		t.Fatalf("first = %q", got)
	}
	if got := SuggestBlockName(stmt, cmap, 2, used); got != "computeTotal2" {
		t.Fatalf("second = %q, want computeTotal2", got)
	}
}

// blockStmtFromSrc parses a function body and returns its first statement plus
// a comment map, so naming heuristics can be exercised on realistic AST input.
func blockStmtFromSrc(t *testing.T, src string) (ast.Stmt, ast.CommentMap) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", "package p\nfunc f() {\n"+src+"\n}\n", parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cmap := ast.NewCommentMap(fset, f, f.Comments)
	fn := f.Decls[0].(*ast.FuncDecl)
	if len(fn.Body.List) == 0 {
		t.Fatalf("no statements parsed from %q", src)
	}
	return fn.Body.List[0], cmap
}
