package parser

import "testing"

func TestParseFile_Method(t *testing.T) {
	content := `package main

type Service struct {
	name string
}

func (s *Service) Start() error {
	return nil
}

func (s Service) GetName() string {
	return s.name
}
`
	tmpFile := createTestFile(t, content)

	info, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile() failed: %v", err)
	}

	if len(info.Methods) != 2 {
		t.Fatalf("Expected 2 methods, got %d", len(info.Methods))
	}

	// Check pointer receiver method
	m1 := info.Methods[0]
	if m1.Name != "Start" {
		t.Errorf("Expected method 'Start', got '%s'", m1.Name)
	}
	if m1.Receiver != "*Service" {
		t.Errorf("Expected receiver '*Service', got '%s'", m1.Receiver)
	}
	if len(m1.Results) != 1 || m1.Results[0].Type != "error" {
		t.Errorf("Expected error return, got %v", m1.Results)
	}

	// Check value receiver method
	m2 := info.Methods[1]
	if m2.Name != "GetName" {
		t.Errorf("Expected method 'GetName', got '%s'", m2.Name)
	}
	if m2.Receiver != "Service" {
		t.Errorf("Expected receiver 'Service', got '%s'", m2.Receiver)
	}
	if len(m2.Results) != 1 || m2.Results[0].Type != "string" {
		t.Errorf("Expected string return, got %v", m2.Results)
	}
}

func TestParseFile_NoParameters(t *testing.T) {
	content := `package main

func noArgs() {
}

func noArgsWithReturn() int {
	return 0
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

	// Check function with no parameters and no return
	if len(info.Functions[0].Parameters) != 0 {
		t.Errorf("Expected 0 parameters, got %d", len(info.Functions[0].Parameters))
	}
	if len(info.Functions[0].Results) != 0 {
		t.Errorf("Expected 0 results, got %d", len(info.Functions[0].Results))
	}

	// Check function with no parameters but with return
	if len(info.Functions[1].Parameters) != 0 {
		t.Errorf("Expected 0 parameters, got %d", len(info.Functions[1].Parameters))
	}
	if len(info.Functions[1].Results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(info.Functions[1].Results))
	}
}

func TestParseFile_UnnamedParameters(t *testing.T) {
	content := `package main

func namedParam(x string, y int) {
}
`
	tmpFile := createTestFile(t, content)

	info, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile() failed: %v", err)
	}

	if len(info.Functions) != 1 {
		t.Fatalf("Expected 1 function, got %d", len(info.Functions))
	}

	fn := info.Functions[0]
	if len(fn.Parameters) != 2 {
		t.Errorf("Expected 2 parameters, got %d", len(fn.Parameters))
	}

	// Check named parameters
	if fn.Parameters[0].Name != "x" || fn.Parameters[0].Type != "string" {
		t.Errorf("Expected param 'x string', got '%s %s'", fn.Parameters[0].Name, fn.Parameters[0].Type)
	}

	if fn.Parameters[1].Name != "y" || fn.Parameters[1].Type != "int" {
		t.Errorf("Expected param 'y int', got '%s %s'", fn.Parameters[1].Name, fn.Parameters[1].Type)
	}
}

func TestParseFile_ComplexNesting(t *testing.T) {
	content := `package data

import (
	"io"
	"time"
)

type Pipeline struct {
	name      string
	stages    []Stage
	config    *Config
	timeout   time.Duration
}

type Stage interface {
	Execute(ctx interface{}) (interface{}, error)
}

type Handler func(interface{}) error

func (p *Pipeline) AddStage(s Stage) error {
	if s == nil {
		return nil
	}
	return nil
}

func (p Pipeline) GetStages() []Stage {
	return p.stages
}

func newPipeline(name string, timeout time.Duration) *Pipeline {
	return nil
}

func processWithReader(r io.Reader, handlers []Handler) map[string]interface{} {
	return nil
}
`
	tmpFile := createTestFile(t, content)

	info, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile() failed: %v", err)
	}

	if info.Package != "data" {
		t.Errorf("Expected package 'data', got '%s'", info.Package)
	}

	if len(info.Imports) != 2 {
		t.Errorf("Expected 2 imports, got %d", len(info.Imports))
	}

	if len(info.Structs) != 1 {
		t.Errorf("Expected 1 struct (Pipeline), got %d", len(info.Structs))
	}

	if len(info.Interfaces) != 1 {
		t.Errorf("Expected 1 interface, got %d", len(info.Interfaces))
	}

	if len(info.Functions) != 2 {
		t.Errorf("Expected 2 functions, got %d", len(info.Functions))
	}

	if len(info.Methods) != 2 {
		t.Errorf("Expected 2 methods, got %d", len(info.Methods))
	}
}
