package parser

import "testing"

func TestParseFile_MultipleElements(t *testing.T) {
	content := `package mypackage

import (
	"fmt"
	"os"
)

type User struct {
	Name  string
	Email string
}

type Reader interface {
	Read(p []byte) (n int, err error)
}

func processUser(u User) {
	fmt.Println(u.Name)
}

func (u *User) GetEmail() string {
	return u.Email
}
`
	tmpFile := createTestFile(t, content)

	info, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile() failed: %v", err)
	}

	// Check package
	if info.Package != "mypackage" {
		t.Errorf("Expected package 'mypackage', got '%s'", info.Package)
	}

	// Check imports
	if len(info.Imports) != 2 {
		t.Errorf("Expected 2 imports, got %d", len(info.Imports))
	}
	if info.Imports[0] != "\"fmt\"" {
		t.Errorf("Expected import 'fmt', got '%s'", info.Imports[0])
	}

	// Check structs
	if len(info.Structs) != 1 {
		t.Errorf("Expected 1 struct, got %d", len(info.Structs))
	}
	if info.Structs[0].Name != "User" {
		t.Errorf("Expected struct 'User', got '%s'", info.Structs[0].Name)
	}
	if len(info.Structs[0].Fields) != 2 {
		t.Errorf("Expected 2 fields, got %d", len(info.Structs[0].Fields))
	}

	// Check interfaces
	if len(info.Interfaces) != 1 {
		t.Errorf("Expected 1 interface, got %d", len(info.Interfaces))
	}
	if info.Interfaces[0].Name != "Reader" {
		t.Errorf("Expected interface 'Reader', got '%s'", info.Interfaces[0].Name)
	}

	// Check functions
	if len(info.Functions) != 1 {
		t.Errorf("Expected 1 function, got %d", len(info.Functions))
	}

	// Check methods
	if len(info.Methods) != 1 {
		t.Errorf("Expected 1 method, got %d", len(info.Methods))
	}
	if info.Methods[0].Receiver != "*User" {
		t.Errorf("Expected receiver '*User', got '%s'", info.Methods[0].Receiver)
	}
}

func TestParseFile_MultipleReturns(t *testing.T) {
	content := `package main

func divideWithError(a int, b int) (int, error) {
	return a / b, nil
}

func multiReturn() (string, int, bool) {
	return "test", 42, true
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

	// Check multiple returns
	if len(info.Functions[0].Results) != 2 {
		t.Errorf("Expected 2 return values, got %d", len(info.Functions[0].Results))
	}

	if info.Functions[0].Results[0].Type != "int" {
		t.Errorf("Expected first return 'int', got '%s'", info.Functions[0].Results[0].Type)
	}

	if info.Functions[0].Results[1].Type != "error" {
		t.Errorf("Expected second return 'error', got '%s'", info.Functions[0].Results[1].Type)
	}

	// Check three returns
	if len(info.Functions[1].Results) != 3 {
		t.Errorf("Expected 3 return values, got %d", len(info.Functions[1].Results))
	}
}

func TestParseFile_InterfaceWithMethods(t *testing.T) {
	content := `package main

type Writer interface {
	Write(p []byte) (n int, err error)
	Close() error
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

	iface := info.Interfaces[0]
	if iface.Name != "Writer" {
		t.Errorf("Expected interface name 'Writer', got '%s'", iface.Name)
	}

	if len(iface.Methods) != 2 {
		t.Errorf("Expected 2 interface methods, got %d", len(iface.Methods))
	}

	if iface.Methods[0].Name != "Write" {
		t.Errorf("Expected method 'Write', got '%s'", iface.Methods[0].Name)
	}

	if len(iface.Methods[0].Parameters) != 1 {
		t.Errorf("Expected 1 parameter for Write, got %d", len(iface.Methods[0].Parameters))
	}

	if iface.Methods[0].Parameters[0].Type != "[]byte" {
		t.Errorf("Expected parameter type '[]byte', got '%s'", iface.Methods[0].Parameters[0].Type)
	}

	if len(iface.Methods[0].Results) != 2 {
		t.Errorf("Expected 2 returns for Write, got %d", len(iface.Methods[0].Results))
	}
}

func TestParseFile_StructWithVariousTypes(t *testing.T) {
	content := `package main

type Config struct {
	Name      string
	Port      int
	Timeout   time.Duration
	Handlers  []Handler
	Metadata  map[string]interface{}
	Parent    *Config
}
`
	tmpFile := createTestFile(t, content)

	info, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile() failed: %v", err)
	}

	if len(info.Structs) != 1 {
		t.Fatalf("Expected 1 struct, got %d", len(info.Structs))
	}

	s := info.Structs[0]
	if s.Name != "Config" {
		t.Errorf("Expected struct 'Config', got '%s'", s.Name)
	}

	if len(s.Fields) != 6 {
		t.Errorf("Expected 6 fields, got %d", len(s.Fields))
	}

	// Verify field types
	expectedTypes := map[int]string{
		0: "string",
		1: "int",
		2: "time.Duration",
		3: "[]Handler",
		4: "map[string]interface{}",
		5: "*Config",
	}

	for idx, expectedType := range expectedTypes {
		if s.Fields[idx].Type != expectedType {
			t.Errorf("Expected field[%d] type '%s', got '%s'", idx, expectedType, s.Fields[idx].Type)
		}
	}
}
