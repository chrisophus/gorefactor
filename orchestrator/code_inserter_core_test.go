package orchestrator

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestFindFunction(t *testing.T) {
	tests := []struct {
		name         string
		code         string
		funcName     string
		methodName   string
		receiverType string
		wantFound    bool
	}{
		{
			name: "find simple function",
			code: `package main

func Hello() {
	println("hello")
}`,
			funcName:  "Hello",
			wantFound: true,
		},
		{
			name: "find method",
			code: `package main

type MyType struct{}

func (m *MyType) DoThing() {
	println("do")
}`,
			methodName:   "DoThing",
			receiverType: "MyType",
			wantFound:    true,
		},
		{
			name: "function not found",
			code: `package main

func Hello() {
}`,
			funcName:  "NotThere",
			wantFound: false,
		},
		{
			name: "find multiple functions",
			code: `package main

func First() {}
func Second() {}
func Third() {}`,
			funcName:  "Second",
			wantFound: true,
		},
		{
			name: "method with pointer receiver",
			code: `package main

type Worker struct{}

func (w *Worker) Process() {}
func (w Worker) Read() {}`,
			methodName:   "Process",
			receiverType: "Worker",
			wantFound:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			node, err := parser.ParseFile(fset, "test.go", tt.code, parser.ParseComments)
			if err != nil {
				t.Fatalf("failed to parse: %v", err)
			}

			ci := NewCodeInserter()
			found := ci.FindFunction(node, tt.funcName, tt.methodName, tt.receiverType)

			if (found != nil) != tt.wantFound {
				t.Errorf("found = %v, want %v", found != nil, tt.wantFound)
			}
		})
	}
}

func TestRemoveCodeBlock(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		location   *InsertionLocation
		pattern    string
		wantErr    bool
		checkAfter func(t *testing.T, fileContent string)
	}{
		{
			name: "remove statement from function",
			input: `package main

func Foo() {
	x := 1
	println("hello")
	y := 2
}`,
			location: &InsertionLocation{
				Type:         "inside_function",
				FunctionName: "Foo",
			},
			pattern: `println("hello")`,
			wantErr: false,
			checkAfter: func(t *testing.T, content string) {
				if contains(content, `println("hello")`) {
					t.Error("removed statement still in file")
				}
				if !contains(content, `x := 1`) {
					t.Error("unrelated code was removed")
				}
			},
		},
		{
			name: "remove statement from method",
			input: `package main

type Worker struct{}

func (w *Worker) Work() {
	println("start")
	println("end")
}`,
			location: &InsertionLocation{
				Type:         "inside_function",
				MethodName:   "Work",
				ReceiverType: "Worker",
			},
			pattern: `println("start")`,
			wantErr: false,
		},
		{
			name: "remove non-existent function",
			input: `package main

func Foo() {}`,
			location: &InsertionLocation{
				Type:         "inside_function",
				FunctionName: "NotThere",
			},
			pattern: `x := 1`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			filePath := filepath.Join(dir, "test.go")
			if err := os.WriteFile(filePath, []byte(tt.input), 0o644); err != nil {
				t.Fatal(err)
			}

			ci := NewCodeInserter()
			_, err := ci.RemoveCodeBlock(filePath, tt.location, tt.pattern)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if !tt.wantErr && tt.checkAfter != nil {
				content, err := os.ReadFile(filePath)
				if err != nil {
					t.Fatal(err)
				}
				tt.checkAfter(t, string(content))
			}
		})
	}
}

func TestReplaceCodeBlock(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		location   *InsertionLocation
		oldPattern string
		newCode    string
		wantErr    bool
		checkAfter func(t *testing.T, content string)
	}{
		{
			name: "replace statement in function",
			input: `package main

func Greet() {
	x := 1
	println("old")
	y := 2
}`,
			location: &InsertionLocation{
				Type:         "inside_function",
				FunctionName: "Greet",
			},
			oldPattern: `println("old")`,
			newCode:    `println("new")`,
			wantErr:    false,
			checkAfter: func(t *testing.T, content string) {
				if !contains(content, `println("new")`) {
					t.Error("new code not found in file")
				}
				if contains(content, `println("old")`) {
					t.Error("old code still in file")
				}
			},
		},
		{
			name: "replace statement in method",
			input: `package main

type Worker struct{}

func (w *Worker) Do() {
	println("old work")
	x := 1
}`,
			location: &InsertionLocation{
				Type:         "inside_function",
				MethodName:   "Do",
				ReceiverType: "Worker",
			},
			oldPattern: `println("old work")`,
			newCode:    `println("new work")`,
			wantErr:    false,
			checkAfter: func(t *testing.T, content string) {
				if !contains(content, `println("new work")`) {
					t.Error("new code not found")
				}
			},
		},
		{
			name: "replace non-existent function",
			input: `package main

func Foo() {}`,
			location: &InsertionLocation{
				Type:         "inside_function",
				FunctionName: "NotThere",
			},
			oldPattern: `x := 1`,
			newCode:    `x := 2`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			filePath := filepath.Join(dir, "test.go")
			if err := os.WriteFile(filePath, []byte(tt.input), 0o644); err != nil {
				t.Fatal(err)
			}

			ci := NewCodeInserter()
			_, err := ci.ReplaceCodeBlock(filePath, tt.location, tt.oldPattern, tt.newCode)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if !tt.wantErr && tt.checkAfter != nil {
				content, err := os.ReadFile(filePath)
				if err != nil {
					t.Fatal(err)
				}
				tt.checkAfter(t, string(content))
			}
		})
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
