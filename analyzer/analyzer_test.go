package analyzer

import (
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
