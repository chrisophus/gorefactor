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
	defer os.Remove(testFile)

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
			info, err := AnalyzeBlock(testFile, tt.startLine, tt.endLine)
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
	defer os.Remove(testFile)

	recommendations, err := RecommendExtractions(testFile)
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
	defer os.Remove(testFile)

	recommendations, err := RecommendExtractions(testFile)
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
	defer os.Remove(testFile)

	recommendations, err := RecommendExtractions(testFile)
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
	defer os.Remove(testFile)

	recommendations, err := RecommendExtractions(testFile)
	if err != nil {
		t.Fatalf("RecommendExtractions() error = %v", err)
	}

	if len(recommendations) < 3 {
		t.Errorf("Expected at least 3 recommended blocks, got %d", len(recommendations))
	}

	for _, rec := range recommendations {
		t.Logf("Realistic extraction recommended block: start=%d, end=%d, vars=%v", rec.StartLine, rec.EndLine, rec.Variables)
	}
}
