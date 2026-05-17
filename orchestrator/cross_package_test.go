package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanMoveSafely(t *testing.T) {
	tests := []struct {
		name         string
		sourceCode   string
		destExists   bool
		destCode     string
		funcName     string
		wantCanMove  bool
		wantWarnings int
		wantErr      bool
	}{
		{
			name: "move unexported function to new file",
			sourceCode: `package myapp

func helper() {
	println("help")
}`,
			destExists:   false,
			funcName:     "helper",
			wantCanMove:  true,
			wantWarnings: 0,
			wantErr:      false,
		},
		{
			name: "move exported function",
			sourceCode: `package myapp

func Helper() {
	println("help")
}`,
			destExists:   false,
			funcName:     "Helper",
			wantCanMove:  true,
			wantWarnings: 1, // warning about exported function
			wantErr:      false,
		},
		{
			name: "move to same package",
			sourceCode: `package myapp

func helper() {
	println("help")
}`,
			destExists: true,
			destCode: `package myapp

func otherFunc() {
}`,
			funcName:     "helper",
			wantCanMove:  true,
			wantWarnings: 0,
			wantErr:      false,
		},
		{
			name: "cannot move to different package",
			sourceCode: `package myapp

func helper() {
}`,
			destExists: true,
			destCode: `package different

func otherFunc() {
}`,
			funcName:     "helper",
			wantCanMove:  false,
			wantWarnings: 1,
			wantErr:      false,
		},
		{
			name: "invalid source file",
			sourceCode: `this is not valid go`,
			destExists: false,
			funcName:   "helper",
			wantCanMove: false,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			sourceFile := filepath.Join(dir, "source.go")
			destFile := filepath.Join(dir, "dest.go")

			if err := os.WriteFile(sourceFile, []byte(tt.sourceCode), 0o644); err != nil {
				t.Fatal(err)
			}

			if tt.destExists {
				if err := os.WriteFile(destFile, []byte(tt.destCode), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			h := NewCrossPackageOperationHandler()
			canMove, warnings, err := h.CanMoveSafely(sourceFile, destFile, tt.funcName)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if canMove != tt.wantCanMove {
				t.Errorf("canMove = %v, want %v", canMove, tt.wantCanMove)
			}

			if len(warnings) != tt.wantWarnings {
				t.Errorf("warnings = %d, want %d (warnings: %v)", len(warnings), tt.wantWarnings, warnings)
			}
		})
	}
}

func TestParseSourceFile(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		wantErr bool
	}{
		{
			name: "parse valid file",
			code: `package main

func Hello() {
	println("hello")
}`,
			wantErr: false,
		},
		{
			name:    "parse invalid go code",
			code:    `this is not valid go`,
			wantErr: true,
		},
		{
			name: "parse file with imports",
			code: `package main

import "fmt"

func main() {
	fmt.Println("hello")
}`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			filePath := filepath.Join(dir, "test.go")
			if err := os.WriteFile(filePath, []byte(tt.code), 0o644); err != nil {
				t.Fatal(err)
			}

			h := NewCrossPackageOperationHandler()
			file, err := h.parseSourceFile(filePath)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if !tt.wantErr && file == nil {
				t.Error("expected AST, got nil")
			}
		})
	}
}

func TestFindFunctionInParsedFile(t *testing.T) {
	code := `package myapp

func First() {}

func Second() {
	println("second")
}

func Third() {
	x := 1
	y := 2
}
`

	tests := []struct {
		name         string
		funcName     string
		wantFound    bool
		expectedCode string
	}{
		{
			name:         "find first function",
			funcName:     "First",
			wantFound:    true,
			expectedCode: "func First() {}",
		},
		{
			name:         "find second function",
			funcName:     "Second",
			wantFound:    true,
			expectedCode: "Second",
		},
		{
			name:      "find non-existent function",
			funcName:  "NotThere",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			filePath := filepath.Join(dir, "test.go")
			if err := os.WriteFile(filePath, []byte(code), 0o644); err != nil {
				t.Fatal(err)
			}

			h := NewCrossPackageOperationHandler()
			file, err := h.parseSourceFile(filePath)
			if err != nil {
				t.Fatalf("failed to parse: %v", err)
			}

			fn, idx, err := h.findFunction(file, tt.funcName, filePath)

			if tt.wantFound {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if fn == nil {
					t.Error("expected function, got nil")
				}
				if idx < 0 {
					t.Errorf("invalid index: %d", idx)
				}
			} else {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if fn != nil {
					t.Error("expected nil, got function")
				}
			}
		})
	}
}

func TestParseDestinationFile(t *testing.T) {
	tests := []struct {
		name         string
		createFile   bool
		code         string
		wantErr      bool
		wantNotFound bool
	}{
		{
			name:       "parse existing file",
			createFile: true,
			code: `package myapp

func Foo() {}`,
			wantErr:      false,
			wantNotFound: false,
		},
		{
			name:         "parse non-existent file",
			createFile:   false,
			wantErr:      false,
			wantNotFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			filePath := filepath.Join(dir, "test.go")

			if tt.createFile {
				if err := os.WriteFile(filePath, []byte(tt.code), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			h := NewCrossPackageOperationHandler()
			file, err := h.parseDestinationFile(filePath)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if tt.wantNotFound {
				if err != ErrFileNotFound {
					t.Errorf("expected ErrFileNotFound, got %v", err)
				}
				if file != nil {
					t.Error("expected nil file, got file")
				}
			} else if !tt.wantErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if file == nil {
					t.Error("expected file, got nil")
				}
			}
		})
	}
}
