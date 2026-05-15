package extractor

import (
	"os"
	"testing"
)

func TestExtractMethod(t *testing.T) {
	// Create a temporary test file
	testFile := "test_file.go"
	content := `package test

type Processor struct {
	value int
}

func (p *Processor) processData(data []int) int {
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
		name       string
		startLine  int
		endLine    int
		methodName string
		wantErr    bool
	}{
		{
			name:       "Valid extraction",
			startLine:  8,
			endLine:    12,
			methodName: "calculateSum",
			wantErr:    false,
		},
		{
			name:       "Invalid block",
			startLine:  1,
			endLine:    1,
			methodName: "invalid",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExtractMethod(testFile, tt.startLine, tt.endLine, tt.methodName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractMethod() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result == nil {
				t.Error("ExtractMethod() returned nil result for valid extraction")
			}
			if !tt.wantErr && result.MethodName != tt.methodName {
				t.Errorf("ExtractMethod() method name = %v, want %v", result.MethodName, tt.methodName)
			}
		})
	}
}

func TestExtractMethodWithVariables(t *testing.T) {
	// Create a temporary test file with variables
	testFile := "test_file.go"
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
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer func() { _ = os.Remove(testFile) }()

	result, err := ExtractMethod(testFile, 6, 8, "calculateTemp")
	if err != nil {
		t.Fatalf("ExtractMethod() error = %v", err)
	}

	// Verify that x was passed as a parameter
	foundX := false
	for _, param := range result.Parameters {
		if param == "x" {
			foundX = true
			break
		}
	}

	if !foundX {
		t.Error("ExtractMethod() did not include 'x' in parameters")
	}

	// Verify that temp was not passed as a parameter (it's assigned in the block)
	for _, param := range result.Parameters {
		if param == "temp" {
			t.Error("ExtractMethod() incorrectly included 'temp' in parameters")
		}
	}
}
