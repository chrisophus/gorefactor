package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// Helper to create temporary test files
func createTestFile(t *testing.T, content string) string {
	tmpDir := os.TempDir()
	tmpFile := filepath.Join(tmpDir, "parser_test_"+t.Name()+".go")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(tmpFile)
	})
	return tmpFile
}

func TestParseFile_SimpleFunction(t *testing.T) {
	content := `package main

func Add(a int, b int) int {
	return a + b
}
`
	tmpFile := createTestFile(t, content)

	info, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile() failed: %v", err)
	}

	if info.Package != "main" {
		t.Errorf("Expected package 'main', got '%s'", info.Package)
	}

	if len(info.Functions) != 1 {
		t.Errorf("Expected 1 function, got %d", len(info.Functions))
	}

	fn := info.Functions[0]
	if fn.Name != "Add" {
		t.Errorf("Expected function name 'Add', got '%s'", fn.Name)
	}

	if len(fn.Parameters) != 2 {
		t.Errorf("Expected 2 parameters, got %d", len(fn.Parameters))
	}

	if fn.Parameters[0].Name != "a" || fn.Parameters[0].Type != "int" {
		t.Errorf("Expected param 'a int', got '%s %s'", fn.Parameters[0].Name, fn.Parameters[0].Type)
	}

	if len(fn.Results) != 1 || fn.Results[0].Type != "int" {
		t.Errorf("Expected 1 int result, got %d results", len(fn.Results))
	}

	if fn.StartLine == 0 {
		t.Error("StartLine should be greater than 0")
	}

	if fn.EndLine <= fn.StartLine {
		t.Error("EndLine should be greater than StartLine")
	}
}

func TestParseFile_PointerTypes(t *testing.T) {
	content := `package main

func createPointer(v int) *int {
	return &v
}

func dereference(p *int) int {
	return *p
}
`
	tmpFile := createTestFile(t, content)

	info, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile() failed: %v", err)
	}

	if len(info.Functions) != 2 {
		t.Fatalf("Expected 2 functions, got %d", len(info.Functions))
	}

	// Check createPointer return type
	if info.Functions[0].Results[0].Type != "*int" {
		t.Errorf("Expected return type '*int', got '%s'", info.Functions[0].Results[0].Type)
	}

	// Check dereference parameter type
	if info.Functions[1].Parameters[0].Type != "*int" {
		t.Errorf("Expected param type '*int', got '%s'", info.Functions[1].Parameters[0].Type)
	}
}

func TestParseFile_ArrayTypes(t *testing.T) {
	content := `package main

func processSlice(items []string) []int {
	return nil
}

func getArray() [10]int {
	return [10]int{}
}
`
	tmpFile := createTestFile(t, content)

	info, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile() failed: %v", err)
	}

	if len(info.Functions) != 2 {
		t.Fatalf("Expected 2 functions, got %d", len(info.Functions))
	}

	// Check slice parameter
	if info.Functions[0].Parameters[0].Type != "[]string" {
		t.Errorf("Expected param type '[]string', got '%s'", info.Functions[0].Parameters[0].Type)
	}

	// Check slice return type
	if info.Functions[0].Results[0].Type != "[]int" {
		t.Errorf("Expected return type '[]int', got '%s'", info.Functions[0].Results[0].Type)
	}
}

func TestParseFile_MapTypes(t *testing.T) {
	content := `package main

func getMap() map[string]int {
	return nil
}

func setMap(m map[string]interface{}) {
}
`
	tmpFile := createTestFile(t, content)

	info, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile() failed: %v", err)
	}

	if len(info.Functions) != 2 {
		t.Fatalf("Expected 2 functions, got %d", len(info.Functions))
	}

	// Check map return type
	if info.Functions[0].Results[0].Type != "map[string]int" {
		t.Errorf("Expected return type 'map[string]int', got '%s'", info.Functions[0].Results[0].Type)
	}

	// Check map parameter type
	if info.Functions[1].Parameters[0].Type != "map[string]interface{}" {
		t.Errorf("Expected param type 'map[string]interface{}', got '%s'", info.Functions[1].Parameters[0].Type)
	}
}

func TestParseFile_SelectorTypes(t *testing.T) {
	content := `package main

import "io"

func readData(r io.Reader) error {
	return nil
}

func writeData(w io.Writer) {
}
`
	tmpFile := createTestFile(t, content)

	info, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile() failed: %v", err)
	}

	if len(info.Functions) != 2 {
		t.Fatalf("Expected 2 functions, got %d", len(info.Functions))
	}

	// Check selector type in parameter
	if info.Functions[0].Parameters[0].Type != "io.Reader" {
		t.Errorf("Expected param type 'io.Reader', got '%s'", info.Functions[0].Parameters[0].Type)
	}

	// Check selector type in result
	if info.Functions[0].Results[0].Type != "error" {
		t.Errorf("Expected return type 'error', got '%s'", info.Functions[0].Results[0].Type)
	}
}
