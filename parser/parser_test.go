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
