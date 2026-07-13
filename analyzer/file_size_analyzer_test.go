package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeFileSize_SmallFile(t *testing.T) {
	content := `package main

func small() {
	x := 1
	return x
}
`
	tmpFile := getTempFile(t, "small")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	issue, err := AnalyzeFileSize(tmpFile, 300)
	if err != nil {
		t.Fatalf("AnalyzeFileSize failed: %v", err)
	}

	if issue.IsOversized {
		t.Error("Small file should not be marked as oversized")
	}

	if issue.LineCount == 0 {
		t.Error("LineCount should be greater than 0")
	}
}

func TestAnalyzeFileSize_LargeFile(t *testing.T) {
	// Create a file with >300 lines
	lines := []string{"package main\n", "\n"}
	for i := 0; i < 150; i++ {
		lines = append(lines, "func fn"+string(rune(i%10))+"() {\n")
		lines = append(lines, "	x := 1\n")
		lines = append(lines, "}\n")
		lines = append(lines, "\n")
	}

	content := strings.Join(lines, "")
	tmpFile := getTempFile(t, "large")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	issue, err := AnalyzeFileSize(tmpFile, 300)
	if err != nil {
		t.Fatalf("AnalyzeFileSize failed: %v", err)
	}

	if !issue.IsOversized {
		t.Errorf("File with %d lines should be marked as oversized", issue.LineCount)
	}

	if issue.OverageSize <= 0 {
		t.Errorf("OverageSize should be positive, got %d", issue.OverageSize)
	}
}

func TestAnalyzeFileSize_DefaultThreshold(t *testing.T) {
	content := `package main

func test() {
}
`
	tmpFile := getTempFile(t, "default")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	issue, err := AnalyzeFileSize(tmpFile, 0) // Use default
	if err != nil {
		t.Fatalf("AnalyzeFileSize failed: %v", err)
	}

	if issue.MaxRecommended != DefaultMaxFileSize {
		t.Errorf("Expected max size %d, got %d", DefaultMaxFileSize, issue.MaxRecommended)
	}
}

func TestAnalyzeFileSize_NonexistentFile(t *testing.T) {
	_, err := AnalyzeFileSize("/nonexistent/file.go", 300)
	if err == nil {
		t.Error("Should return error for nonexistent file")
	}
}

func TestAnalyzeFileSize_ExtractionHints(t *testing.T) {
	// Create file with large functions (>20 lines)
	lines := []string{"package main\n\n"}
	// Add a large function (>20 lines)
	lines = append(lines, "func largeFunction() {\n")
	for i := 0; i < 25; i++ {
		lines = append(lines, "	x := "+string(rune('0'+i%10))+"\n")
	}
	lines = append(lines, "}\n\n")
	// Add another function
	lines = append(lines, "func smallFunc() {\n")
	lines = append(lines, "	return\n")
	lines = append(lines, "}\n")

	content := strings.Join(lines, "")
	tmpFile := getTempFile(t, "hints")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	issue, err := AnalyzeFileSize(tmpFile, 30) // Low threshold to trigger hints
	if err != nil {
		t.Fatalf("AnalyzeFileSize failed: %v", err)
	}

	if !issue.IsOversized {
		t.Errorf("File with %d lines should be marked as oversized (threshold=30)", issue.LineCount)
	}
}

func TestCountLines(t *testing.T) {
	content := `line1
line2
line3
line4
`
	tmpFile := getTempFile(t, "count")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	count, err := countLines(tmpFile)
	if err != nil {
		t.Fatalf("countLines failed: %v", err)
	}

	if count != 4 {
		t.Errorf("Expected 4 lines, got %d", count)
	}
}

func TestCountLines_EmptyFile(t *testing.T) {
	tmpFile := getTempFile(t, "empty")
	if err := os.WriteFile(tmpFile, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	count, err := countLines(tmpFile)
	if err != nil {
		t.Fatalf("countLines failed: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected 0 lines, got %d", count)
	}
}

func TestCalculateFunctionComplexity(t *testing.T) {
	testCases := []struct {
		name        string
		code        string
		expectedMin int // Minimum expected complexity
	}{
		{
			name:        "Simple function",
			code:        "func f() { x := 1 }",
			expectedMin: 1,
		},
		{
			name:        "Function with if",
			code:        "func f() { if true { x := 1 } }",
			expectedMin: 2,
		},
		{
			name:        "Function with for loop",
			code:        "func f() { for i := 0; i < 10; i++ { } }",
			expectedMin: 2,
		},
		{
			name:        "Function with switch",
			code:        "func f() { switch x { case 1: } }",
			expectedMin: 3, // switch gets higher penalty
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			full := "package main\n\n" + tc.code
			file, err := parser.ParseFile(fset, "test.go", full, 0)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			fn := file.Decls[0].(*ast.FuncDecl)
			complexity := calculateFunctionComplexity(fn)

			if complexity < tc.expectedMin {
				t.Errorf("Expected complexity >= %d, got %d", tc.expectedMin, complexity)
			}
		})
	}
}

func TestCalculateExtractionPriority(t *testing.T) {
	testCases := []struct {
		lineCount   int
		complexity  int
		minExpected int // We check minimum, as exact values depend on formula
	}{
		{20, 1, 2},   // Small, simple
		{50, 5, 4},   // Medium, moderate
		{100, 15, 8}, // Large, complex
	}

	for _, tc := range testCases {
		priority := calculateExtractionPriority(tc.lineCount, tc.complexity)
		if priority < tc.minExpected {
			t.Errorf("Lines=%d, Complexity=%d: Expected priority >= %d, got %d",
				tc.lineCount, tc.complexity, tc.minExpected, priority)
		}
		if priority > 10 {
			t.Errorf("Lines=%d, Complexity=%d: Priority should be capped at 10, got %d",
				tc.lineCount, tc.complexity, priority)
		}
	}
}

func getTempFile(t *testing.T, suffix string) string {
	tmpDir := os.TempDir()
	tmpFile := filepath.Join(tmpDir, "size_analyzer_test_"+t.Name()+"_"+suffix+".go")
	t.Cleanup(func() {
		_ = os.Remove(tmpFile)
	})
	return tmpFile
}
