package parser

import (
	"os"
	"path/filepath"
	"testing"
)

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

	// Check fixed-size array return type: the length must be preserved exactly.
	if len(info.Functions[1].Results) != 1 {
		t.Fatalf("Expected 1 result for getArray, got %d", len(info.Functions[1].Results))
	}
	if info.Functions[1].Results[0].Type != "[10]int" {
		t.Errorf("Expected return type '[10]int', got '%s'", info.Functions[1].Results[0].Type)
	}

}
func TestParseFile_GroupedParameters(t *testing.T) {
	content := `package main

func Add(a, b int) int {
	return a + b
}

func mixed(x, y string, z int, w ...bool) {}
`
	tmpFile := createTestFile(t, content)

	info, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile() failed: %v", err)
	}

	if len(info.Functions) != 2 {
		t.Fatalf("Expected 2 functions, got %d", len(info.Functions))
	}

	add := info.Functions[0]
	if len(add.Parameters) != 2 {
		t.Fatalf("Expected 2 parameters for Add, got %d", len(add.Parameters))
	}
	if add.Parameters[0].Name != "a" || add.Parameters[0].Type != "int" {
		t.Errorf("Expected param 'a int', got '%s %s'", add.Parameters[0].Name, add.Parameters[0].Type)
	}
	if add.Parameters[1].Name != "b" || add.Parameters[1].Type != "int" {
		t.Errorf("Expected param 'b int', got '%s %s'", add.Parameters[1].Name, add.Parameters[1].Type)
	}

	mixed := info.Functions[1]
	want := []Param{
		{Name: "x", Type: "string"},
		{Name: "y", Type: "string"},
		{Name: "z", Type: "int"},
		{Name: "w", Type: "...bool"},
	}
	if len(mixed.Parameters) != len(want) {
		t.Fatalf("Expected %d parameters for mixed, got %d", len(want), len(mixed.Parameters))
	}
	for i, w := range want {
		if mixed.Parameters[i] != w {
			t.Errorf("param[%d]: expected %+v, got %+v", i, w, mixed.Parameters[i])
		}
	}
}

func TestParseFile_VariadicAndFuncTypeParams(t *testing.T) {
	content := `package main

func join(sep string, parts ...string) string {
	return ""
}

func apply(f func(int) error, ch chan<- int) error {
	return f(0)
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

	join := info.Functions[0]
	if len(join.Parameters) != 2 {
		t.Fatalf("Expected 2 parameters for join, got %d", len(join.Parameters))
	}
	if join.Parameters[1].Name != "parts" || join.Parameters[1].Type != "...string" {
		t.Errorf("Expected param 'parts ...string', got '%s %s'", join.Parameters[1].Name, join.Parameters[1].Type)
	}

	apply := info.Functions[1]
	if len(apply.Parameters) != 2 {
		t.Fatalf("Expected 2 parameters for apply, got %d", len(apply.Parameters))
	}
	if apply.Parameters[0].Type != "func(int) error" {
		t.Errorf("Expected param type 'func(int) error', got '%s'", apply.Parameters[0].Type)
	}
	if apply.Parameters[1].Type != "chan<- int" {
		t.Errorf("Expected param type 'chan<- int', got '%s'", apply.Parameters[1].Type)
	}
}

func TestParseFile_GroupedStructFields(t *testing.T) {
	content := `package main

type Point struct {
	X, Y int
	Name string
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

	want := []Field{
		{Name: "X", Type: "int"},
		{Name: "Y", Type: "int"},
		{Name: "Name", Type: "string"},
	}
	fields := info.Structs[0].Fields
	if len(fields) != len(want) {
		t.Fatalf("Expected %d fields, got %d", len(want), len(fields))
	}
	for i, w := range

	// Check map return type
	want {
		if fields[i] != w {
			t.Errorf("field[%d]: expected %+v, got %+v", i, w, fields[i])
		}
	}
}

func TestParseFile_EmbeddedInterface(t *testing.T) {
	content := `package main

import "io"

type ReadCloser interface {
	io.Reader
	Closer
	Close() error
}

type Closer interface {
	Close() error
}
`

	// Check map parameter type
	tmpFile := createTestFile(t, content)

	info, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile() failed: %v", err)
	}

	if len(info.Interfaces) != 2 {
		t.Fatalf("Expected 2 interfaces, got %d", len(info.Interfaces))
	}

	rc := info.Interfaces[0]
	if rc.Name != "ReadCloser" {
		t.Fatalf("Expected interface 'ReadCloser', got '%s'", rc.Name)
	}

	wantEmbedded := []string{"io.Reader", "Closer"}
	if len(rc.Embedded) !=

		// Check selector type in parameter
		len(wantEmbedded) {
		t.Fatalf("Expected embedded %v, got %v", wantEmbedded, rc.Embedded)
	}
	for i, w := range wantEmbedded {
		if rc.Embedded[i] != w {
			t.Errorf("embedded[%d]: expected '%s', got '%s'",

				// Check selector type in result
				i, w, rc.Embedded[i])
		}
	}

	if len(rc.Methods) != 1 || rc.Methods[0].Name != "Close" {
		t.Errorf(

			// Helper to create temporary test files
			"Expected 1 method 'Close', got %+v", rc.Methods)
	}

	if len(info.Interfaces[1].Embedded) != 0 {
		t.Errorf("Expected no embedded interfaces for Closer, got %v", info.Interfaces[1].Embedded)
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

	if info.Functions[0].Results[0].Type != "map[string]int" {
		t.Errorf("Expected return type 'map[string]int', got '%s'", info.Functions[0].Results[0].Type)
	}

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

	if info.Functions[0].Parameters[0].Type != "io.Reader" {
		t.Errorf("Expected param type 'io.Reader', got '%s'", info.Functions[0].Parameters[0].Type)
	}

	if info.Functions[0].Results[0].Type != "error" {
		t.Errorf("Expected return type 'error', got '%s'", info.Functions[0].Results[0].Type)
	}
}

func createTestFile(t *testing.T, content string) string {
	t.Helper()
	tmpFile := filepath.Join(t.TempDir(), "src.go")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	return tmpFile

}
