package analyzer

import (
	"os"
	"testing"
)

func TestRecommendExtractionsWithConfig(t *testing.T) {

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
			expectedCount: 5,
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

			for i, rec := range recommendations {
				t.Logf("Recommendation %d: start=%d, end=%d, complexity=%d, statements=%d, readVars=%d, writeVars=%d",
					i+1, rec.StartLine, rec.EndLine, rec.Complexity, rec.StatementCount,
					len(rec.ReadVars), len(rec.WriteVars))
			}
		})
	}
}
