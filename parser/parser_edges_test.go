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

// TestParseFile_EmptyReceiverList pins the guard in parseMethod: go/parser
// accepts `func () orphan() {}` (receiver present but with zero fields), so
// fn.Recv.List can be empty and indexing it unguarded would panic. Found via
// mutation testing: the `len(fn.Recv.List) > 0` branch had no test forcing
// the empty case.
func TestParseFile_EmptyReceiverList(t *testing.T) {
	content := `package p

func () orphan() {}
`
	tmpFile := createTestFile(t, content)

	info, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile() failed: %v", err)
	}
	if len(info.Methods) != 1 {
		t.Fatalf("Expected 1 method, got %d", len(info.Methods))
	}
	if info.Methods[0].Receiver != "" {
		t.Errorf("Expected empty receiver, got %q", info.Methods[0].Receiver)
	}
	if info.Methods[0].Name != "orphan" {
		t.Errorf("Expected method 'orphan', got %q", info.Methods[0].Name)
	}
}

// TestParseFile_InterfaceNoEmbedded pins Embedded to nil (not an empty
// slice) when an interface embeds nothing, so JSON output omits the field
// via omitempty. Found via mutation testing: the `len(embedded) == 0`
// normalization had no test observing nil-ness.
func TestParseFile_InterfaceNoEmbedded(t *testing.T) {
	content := `package p

type Runner interface {
	Run() error
}
`
	tmpFile := createTestFile(t, content)

	info, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile() failed: %v", err)
	}
	if len(info.Interfaces) != 1 {
		t.Fatalf("Expected 1 interface, got %d", len(info.Interfaces))
	}
	if info.Interfaces[0].Embedded != nil {
		t.Errorf("Expected Embedded to be nil for interface with no embedded types, got %#v", info.Interfaces[0].Embedded)
	}
}
