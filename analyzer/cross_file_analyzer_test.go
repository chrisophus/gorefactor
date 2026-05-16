package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"

	"strings"
	"testing"
)

func TestNormalizeCode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "removes comments",
			input:    "x := 5 // comment\ny := 10",
			expected: "VAR := 5 VAR := 10",
		},
		{
			name:     "removes extra whitespace",
			input:    "x  :=   5",
			expected: "VAR := 5",
		},
		{
			name:     "preserves keywords",
			input:    "if x > 5 { return }",
			expected: "if VAR > 5 { return }",
		},
		{
			name:     "replaces variable names",
			input:    "count := 0\ntotal := 10",
			expected: "VAR := 0 VAR := 10",
		},
		{
			name:     "mixed keywords and variables",
			input:    "for i := 0; i < count; i++ { sum += val }",
			expected: "for VAR := 0; VAR < VAR VAR { VAR += VAR }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeCode(tt.input)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestHashCode(t *testing.T) {
	code1 := "x := 5"
	code2 := "x := 5"
	code3 := "x := 6"

	hash1 := hashCode(code1)
	hash2 := hashCode(code2)
	hash3 := hashCode(code3)

	if hash1 != hash2 {
		t.Errorf("identical code should have identical hash")
	}

	if hash1 == hash3 {
		t.Errorf("different code should have different hash")
	}

	if hash1 == "" || hash2 == "" || hash3 == "" {
		t.Errorf("hash should not be empty")
	}
}

func TestCountStatements(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected int
	}{
		{
			name: "simple assignment",
			code: `{
				x := 5
			}`,
			expected: 1,
		},
		{
			name: "multiple statements",
			code: `{
				x := 5
				y := 10
				z := x + y
			}`,
			expected: 3,
		},
		{
			name: "if statement",
			code: `{
				if x > 5 {
					y := 10
				}
			}`,
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the code
			src := "package main\nfunc test() " + tt.code
			fset := token.NewFileSet()
			node, err := parser.ParseFile(fset, "test.go", src, 0)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			// Find the function
			var fn *ast.FuncDecl
			for _, decl := range node.Decls {
				if f, ok := decl.(*ast.FuncDecl); ok {
					fn = f
					break
				}
			}

			if fn == nil {
				t.Fatal("function not found")
			}

			result := countStatements(fn.Body)
			if result != tt.expected {
				t.Errorf("got %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestCalculateImpactScore(t *testing.T) {
	tests := []struct {
		name            string
		locations       int
		statements      int
		complexity      int
		expectedInRange bool // just check it's reasonable
	}{
		{
			name:            "duplicate in 2 files",
			locations:       2,
			statements:      10,
			complexity:      2,
			expectedInRange: true,
		},
		{
			name:            "duplicate in 3 files",
			locations:       3,
			statements:      20,
			complexity:      5,
			expectedInRange: true,
		},
		{
			name:            "single location (no duplicate)",
			locations:       1,
			statements:      10,
			complexity:      2,
			expectedInRange: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := calculateImpactScore(tt.locations, tt.statements, tt.complexity)
			if score < 0 || score > 1000 {
				t.Errorf("score %d out of reasonable range [0, 1000]", score)
			}
		})
	}
}

func TestGenerateDuplicateRecommendation(t *testing.T) {
	locations := []Location{
		{File: "/path/to/file1.go", StartLine: 10, EndLine: 20},
		{File: "/path/to/file2.go", StartLine: 50, EndLine: 60},
	}

	recommendation := generateDuplicateRecommendation(locations)

	if !strings.Contains(recommendation, "file1.go") {
		t.Errorf("recommendation should contain first file name")
	}
	if !strings.Contains(recommendation, "file2.go") {
		t.Errorf("recommendation should contain second file name")
	}
	if !strings.Contains(recommendation, "Extract to shared utility") {
		t.Errorf("recommendation should include suggestion")
	}
}

func TestEstimateSavings(t *testing.T) {
	duplicates := []DuplicateBlock{
		{
			StatementCount: 10,
			Locations: []Location{
				{File: "file1.go"},
				{File: "file2.go"},
				{File: "file3.go"},
			},
		},
		{
			StatementCount: 5,
			Locations: []Location{
				{File: "file4.go"},
				{File: "file5.go"},
			},
		},
	}

	savings := estimateSavings(duplicates)

	// Should have saved (3-1)*10 + (2-1)*5 = 20 + 5 = 25 lines
	if !strings.Contains(savings, "25") {
		t.Errorf("savings should mention ~25 lines, got: %s", savings)
	}
}
