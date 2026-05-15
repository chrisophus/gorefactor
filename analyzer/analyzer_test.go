package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"
)

func TestAnalyzeBlock(t *testing.T) {
	// Create a temporary test file
	testFile := "test_file.go"
	content := `package test

func processData(data []int) int {
	sum := 0
	for i := 0; i < len(data); i++ {
		if data[i] > 0 {
			sum += data[i]
		}
	}
	return sum
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer func() { _ = os.Remove(testFile) }()

	// Test cases
	tests := []struct {
		name      string
		startLine int
		endLine   int
		want      bool
	}{
		{
			name:      "Valid block",
			startLine: 5,
			endLine:   9,
			want:      true,
		},
		{
			name:      "Too small block",
			startLine: 5,
			endLine:   6,
			want:      false,
		},
		{
			name:      "Invalid block",
			startLine: 1,
			endLine:   1,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := AnalyzeBlock(testFile, tt.startLine, tt.endLine, DefaultConfig())
			if err != nil {
				t.Fatalf("AnalyzeBlock() error = %v", err)
			}
			if info == nil && tt.want {
				t.Error("AnalyzeBlock() returned nil for valid block")
			}
			if info != nil && info.IsExtractable != tt.want {
				t.Errorf("AnalyzeBlock() IsExtractable = %v, want %v", info.IsExtractable, tt.want)
			}
		})
	}
}

func TestRecommendExtractions(t *testing.T) {
	// Create a temporary test file
	testFile := "test_file.go"
	content := `package test

func processData(data []int) int {
	sum := 0
	for i := 0; i < len(data); i++ {
		if data[i] > 0 {
			sum += data[i]
		}
	}
	return sum
}

func complexFunction(x, y int) int {
	result := 0
	if x > 0 {
		temp := x * 2
		if y > 0 {
			result = temp + y
		}
	}
	return result
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer func() { _ = os.Remove(testFile) }()

	recommendations, err := RecommendExtractions(testFile, "", DefaultConfig())
	if err != nil {
		t.Fatalf("RecommendExtractions() error = %v", err)
	}

	if len(recommendations) == 0 {
		t.Error("RecommendExtractions() returned no recommendations")
	}

	// Print all recommended block line numbers for debugging
	for _, rec := range recommendations {
		t.Logf("Recommended block: start=%d, end=%d", rec.StartLine, rec.EndLine)
	}

	// Verify that we found the nested if block in complexFunction
	foundNestedBlock := false
	for _, rec := range recommendations {
		if rec.StartLine == 15 && rec.EndLine == 20 {
			foundNestedBlock = true
			break
		}
	}

	if !foundNestedBlock {
		t.Error("RecommendExtractions() did not find the nested if block")
	}
}

func TestRecommendExtractions_LargeFunction(t *testing.T) {
	testFile := "test_large.go"
	content := `package test

func bigFunction(a, b, c, d, e int, arr []int) int {
	x := a + b
	y := c + d
	z := e
	result := 0
	for i := 0; i < len(arr); i++ {
		if arr[i]%2 == 0 {
			result += arr[i]
		}
	}
	if result > 10 {
		result += x + y + z
	}
	return result
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer func() { _ = os.Remove(testFile) }()

	recommendations, err := RecommendExtractions(testFile, "", DefaultConfig())
	if err != nil {
		t.Fatalf("RecommendExtractions() error = %v", err)
	}

	if len(recommendations) == 0 {
		t.Error("RecommendExtractions() returned no recommendations for large function")
	}

	for _, rec := range recommendations {
		t.Logf("Large function recommended block: start=%d, end=%d, vars=%v", rec.StartLine, rec.EndLine, rec.Variables)
	}

	// Check that at least one block uses multiple variables
	found := false
	for _, rec := range recommendations {
		if len(rec.Variables) >= 2 {
			found = true
			break
		}
	}
	if !found {
		t.Error("No recommended block in large function uses multiple variables")
	}
}

func TestRecommendExtractions_ManyVariables(t *testing.T) {
	testFile := "test_manyvars.go"
	content := `package test

func manyVars(a, b, c, d, e, f, g, h, i, j int) int {
	sum := 0
	if a > 0 {
		sum += a
	}
	if b > 0 {
		sum += b
	}
	if c > 0 {
		sum += c
	}
	if d > 0 {
		sum += d
	}
	if e > 0 {
		sum += e
	}
	if f > 0 {
		sum += f
	}
	if g > 0 {
		sum += g
	}
	if h > 0 {
		sum += h
	}
	if i > 0 {
		sum += i
	}
	if j > 0 {
		sum += j
	}
	return sum
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer func() { _ = os.Remove(testFile) }()

	recommendations, err := RecommendExtractions(testFile, "", DefaultConfig())
	if err != nil {
		t.Fatalf("RecommendExtractions() error = %v", err)
	}

	for _, rec := range recommendations {
		t.Logf("ManyVars recommended block: start=%d, end=%d, vars=%v", rec.StartLine, rec.EndLine, rec.Variables)
	}
}

func TestRecommendExtractions_RealisticExtraction(t *testing.T) {
	testFile := "test_realistic.go"
	content := `package test

func complexFunction(a, b, c, d, e int, arr []int) int {
	x := a + b
	y := c + d
	z := e
	result := 0
	for i := 0; i < len(arr); i++ {
		if arr[i]%2 == 0 {
			result += arr[i]
		}
	}
	if result > 10 {
		result += x + y + z
	}
	return result
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer func() { _ = os.Remove(testFile) }()

	recommendations, err := RecommendExtractions(testFile, "", DefaultConfig())
	if err != nil {
		t.Fatalf("RecommendExtractions() error = %v", err)
	}

	if len(recommendations) < 2 {
		t.Errorf("Expected at least 2 recommended blocks, got %d", len(recommendations))
	}

	for _, rec := range recommendations {
		t.Logf("Realistic extraction recommended block: start=%d, end=%d, vars=%v", rec.StartLine, rec.EndLine, rec.Variables)
	}

	// Debug: Print all analyzed blocks and their complexity
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, testFile, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse test file: %v", err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		block, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		startLine := fset.Position(block.Pos()).Line
		endLine := fset.Position(block.End()).Line
		info, err := AnalyzeBlock(testFile, startLine, endLine, DefaultConfig())
		if err != nil {
			return true
		}
		t.Logf("Analyzed block: start=%d, end=%d, complexity=%d, extractable=%v", startLine, endLine, info.Complexity, info.IsExtractable)
		return true
	})
}

func TestRecommendExtractionsWithConfig(t *testing.T) {
	// Create a temporary test file
	testFile := "test_config.go"
	content := `package test

func complexFunction(x, y int) int {
	result := 0
	if x > 0 {
		temp := x * 2
		if y > 0 {
			result = temp + y
		}
	}
	return result
}

func simpleFunction(a, b int) int {
	return a + b
}

func manyVarsFunction(a, b, c, d, e, f, g, h, i, j, k, l, m, n, o, p, q, r, s, t, u int) int {
	sum := 0
	if a > 0 {
		sum += a
	}
	return sum
}

func manyStatementsFunction(x int) int {
	result := 0
	for i := 0; i < 100; i++ {
		result += i
		if result > 1000 {
			break
		}
	}
	return result
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer func() { _ = os.Remove(testFile) }()

	tests := []struct {
		name          string
		config        *ExtractionConfig
		expectedCount int
		checkFunction func([]*BlockInfo) bool
		description   string
	}{
		{
			name:          "Default config",
			config:        DefaultConfig(),
			expectedCount: 5, // Should find all blocks including duplicates from leading statements
			checkFunction: func(blocks []*BlockInfo) bool {
				return len(blocks) > 0
			},
			description: "Default configuration should find reasonable blocks",
		},
		{
			name: "High complexity threshold",
			config: &ExtractionConfig{
				MinComplexity: 10,
				MaxComplexity: 20,
				MaxReadVars:   20,
				MaxWriteVars:  10,
				MinStatements: 3,
				MaxStatements: 50,
			},
			expectedCount: 0,
			checkFunction: func(blocks []*BlockInfo) bool {
				return len(blocks) == 0
			},
			description: "High complexity threshold should find no blocks",
		},
		{
			name: "Low variable threshold",
			config: &ExtractionConfig{
				MinComplexity: 1,
				MaxComplexity: 10,
				MaxReadVars:   3,
				MaxWriteVars:  1,
				MinStatements: 3,
				MaxStatements: 50,
			},
			expectedCount: 0,
			checkFunction: func(blocks []*BlockInfo) bool {
				return len(blocks) == 0
			},
			description: "Low variable threshold should find no blocks due to strict limits",
		},
		{
			name: "Statement count limits",
			config: &ExtractionConfig{
				MinComplexity: 1,
				MaxComplexity: 10,
				MaxReadVars:   20,
				MaxWriteVars:  10,
				MinStatements: 20,
				MaxStatements: 30,
			},
			expectedCount: 0,
			checkFunction: func(blocks []*BlockInfo) bool {
				return len(blocks) == 0
			},
			description: "Statement count limits should find no blocks due to strict limits",
		},
		{
			name: "Zero leading statements",
			config: &ExtractionConfig{
				MinComplexity:   1,
				MaxComplexity:   10,
				MaxReadVars:     20,
				MaxWriteVars:    10,
				MinStatements:   3,
				MaxStatements:   50,
				NumLeadingStmts: 0,
			},
			expectedCount: 5,
			checkFunction: func(blocks []*BlockInfo) bool {
				return len(blocks) > 0
			},
			description: "Zero leading statements should find all blocks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recommendations, err := RecommendExtractions(testFile, "", tt.config)
			if err != nil {
				t.Fatalf("RecommendExtractions() error = %v", err)
			}

			if len(recommendations) != tt.expectedCount {
				t.Errorf("Got %d recommendations, want %d", len(recommendations), tt.expectedCount)
			}

			if !tt.checkFunction(recommendations) {
				t.Errorf("Check function failed: %s", tt.description)
			}

			// Print details of recommendations for debugging
			for i, rec := range recommendations {
				t.Logf("Recommendation %d: start=%d, end=%d, complexity=%d, statements=%d, readVars=%d, writeVars=%d",
					i+1, rec.StartLine, rec.EndLine, rec.Complexity, rec.StatementCount,
					len(rec.ReadVars), len(rec.WriteVars))
			}
		})
	}
}

func TestAnalyzeBlockWithConfig(t *testing.T) {
	// Create a temporary test file
	testFile := "test_analyze.go"
	content := `package test

func testFunction(x, y int) int {
	result := 0
	if x > 0 {
		temp := x * 2
		if y > 0 {
			result = temp + y
		}
	}
	return result
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer func() { _ = os.Remove(testFile) }()

	tests := []struct {
		name          string
		startLine     int
		endLine       int
		config        *ExtractionConfig
		shouldExtract bool
		description   string
	}{
		{
			name:      "Default config - nested if block",
			startLine: 7,
			endLine:   9,
			config: &ExtractionConfig{
				MinComplexity: 1,
				MaxComplexity: 10,
				MaxReadVars:   20,
				MaxWriteVars:  10,
				MinStatements: 2,
				MaxStatements: 50,
			},
			shouldExtract: true,
			description:   "Default config should extract nested if block",
		},
		{
			name:      "High complexity threshold - nested if block",
			startLine: 7,
			endLine:   9,
			config: &ExtractionConfig{
				MinComplexity: 3,
				MaxComplexity: 10,
				MaxReadVars:   20,
				MaxWriteVars:  10,
				MinStatements: 3,
				MaxStatements: 50,
			},
			shouldExtract: false,
			description:   "High complexity threshold should not extract nested if block",
		},
		{
			name:      "Low variable threshold - nested if block",
			startLine: 7,
			endLine:   9,
			config: &ExtractionConfig{
				MinComplexity: 1,
				MaxComplexity: 10,
				MaxReadVars:   1,
				MaxWriteVars:  1,
				MinStatements: 3,
				MaxStatements: 50,
			},
			shouldExtract: false,
			description:   "Low variable threshold should not extract nested if block",
		},
		{
			name:      "Statement count limits - nested if block",
			startLine: 7,
			endLine:   9,
			config: &ExtractionConfig{
				MinComplexity: 1,
				MaxComplexity: 10,
				MaxReadVars:   20,
				MaxWriteVars:  10,
				MinStatements: 10,
				MaxStatements: 20,
			},
			shouldExtract: false,
			description:   "Statement count limits should not extract nested if block",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := AnalyzeBlock(testFile, tt.startLine, tt.endLine, tt.config)
			if err != nil {
				t.Fatalf("AnalyzeBlock() error = %v", err)
			}

			if info == nil {
				t.Fatal("AnalyzeBlock() returned nil info")
			}

			if info.IsExtractable != tt.shouldExtract {
				t.Errorf("IsExtractable = %v, want %v: %s", info.IsExtractable, tt.shouldExtract, tt.description)
			}

			// Print details of the analysis for debugging
			t.Logf("Analysis: complexity=%d, statements=%d, readVars=%d, writeVars=%d",
				info.Complexity, info.StatementCount, len(info.ReadVars), len(info.WriteVars))
		})
	}
}
