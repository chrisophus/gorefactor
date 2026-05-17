package parser

import "testing"

func TestParseFile_NonExistentFile(t *testing.T) {
	_, err := ParseFile("/nonexistent/file/path.go")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}

func TestParseFile_InvalidGo(t *testing.T) {
	content := `package main

func unclosed(
`
	tmpFile := createTestFile(t, content)

	_, err := ParseFile(tmpFile)
	if err == nil {
		t.Error("Expected error for invalid Go code, got nil")
	}
}

func TestParseFile_EmptyFile(t *testing.T) {
	content := `package main
`
	tmpFile := createTestFile(t, content)

	info, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile() failed: %v", err)
	}

	if info.Package != "main" {
		t.Errorf("Expected package 'main', got '%s'", info.Package)
	}

	if len(info.Functions) != 0 {
		t.Errorf("Expected 0 functions, got %d", len(info.Functions))
	}

	if len(info.Methods) != 0 {
		t.Errorf("Expected 0 methods, got %d", len(info.Methods))
	}

	if len(info.Structs) != 0 {
		t.Errorf("Expected 0 structs, got %d", len(info.Structs))
	}

	if len(info.Interfaces) != 0 {
		t.Errorf("Expected 0 interfaces, got %d", len(info.Interfaces))
	}
}
