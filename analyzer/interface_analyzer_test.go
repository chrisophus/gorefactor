package analyzer

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestFindImplementations(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

type Reader interface {
	Read(p []byte) (n int, err error)
}

type MyReader struct {
	data string
}

func (mr *MyReader) Read(p []byte) (int, error) {
	return len(p), nil
}

type Writer struct {
	buffer string
}

func (w *Writer) Write(p []byte) (int, error) {
	return len(p), nil
}
`
	ioutil.WriteFile(testFile, []byte(content), 0644)

	ia := NewInterfaceAnalyzer([]string{testFile})
	analysis, err := ia.FindImplementations("Reader")

	if err != nil {
		t.Fatalf("FindImplementations error: %v", err)
	}

	if analysis == nil {
		t.Fatal("analysis should not be nil")
	}

	// Should find MyReader as implementation
	if len(analysis.Implementations) != 1 {
		t.Errorf("expected 1 implementation, got %d", len(analysis.Implementations))
	}

	if analysis.Implementations[0].TypeName != "MyReader" {
		t.Errorf("expected MyReader, got %s", analysis.Implementations[0].TypeName)
	}

	// Verify implemented methods
	if len(analysis.Implementations[0].ImplementedMethods) != 1 {
		t.Errorf("expected 1 implemented method, got %d", len(analysis.Implementations[0].ImplementedMethods))
	}

	// Writer should not be found as it doesn't implement Reader
	if len(analysis.PartialImplements) > 0 || len(analysis.Implementations) > 1 {
		t.Errorf("Writer should not be included as an implementation")
	}
}

func TestVerifyInterfaceImpl(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

type Reader interface {
	Read(p []byte) (n int, err error)
	Close() error
}

type MyReader struct{}

func (mr *MyReader) Read(p []byte) (int, error) {
	return 0, nil
}

func (mr *MyReader) Close() error {
	return nil
}

type PartialReader struct{}

func (pr *PartialReader) Read(p []byte) (int, error) {
	return 0, nil
}
`
	ioutil.WriteFile(testFile, []byte(content), 0644)

	ia := NewInterfaceAnalyzer([]string{testFile})

	// Test complete implementation
	implements, missing, err := ia.VerifyInterfaceImpl("MyReader", "Reader")
	if err != nil {
		t.Fatalf("VerifyInterfaceImpl error: %v", err)
	}

	if !implements {
		t.Errorf("MyReader should implement Reader")
	}

	if len(missing) != 0 {
		t.Errorf("MyReader should not have missing methods, got %v", missing)
	}

	// Test partial implementation
	implements, missing, err = ia.VerifyInterfaceImpl("PartialReader", "Reader")
	if err != nil {
		t.Fatalf("VerifyInterfaceImpl error: %v", err)
	}

	if implements {
		t.Errorf("PartialReader should not fully implement Reader")
	}

	if len(missing) != 1 || missing[0] != "Close" {
		t.Errorf("PartialReader should be missing Close method, got %v", missing)
	}
}

func TestFindInterfaceUsers(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

type Reader interface {
	Read(p []byte) (n int, err error)
}

func ProcessReader(r Reader) {
	data := make([]byte, 10)
	r.Read(data)
}

var myReader Reader
`
	ioutil.WriteFile(testFile, []byte(content), 0644)

	ia := NewInterfaceAnalyzer([]string{testFile})

	// Parse first
	if err := ia.symbolAnalyzer.Parse(); err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	ia.symbolAnalyzer.collectDefinitions()

	// Test that function can find interface definition
	interfaceInfo := ia.findInterfaceDefinition("Reader")
	if interfaceInfo == nil {
		t.Errorf("expected to find Reader interface definition")
	}

	if interfaceInfo != nil && interfaceInfo.Name != "Reader" {
		t.Errorf("expected interface name Reader, got %s", interfaceInfo.Name)
	}
}

func TestMultipleImplementations(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

type Writer interface {
	Write(p []byte) (n int, err error)
}

type FileWriter struct{}

func (fw *FileWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

type BufferWriter struct{}

func (bw *BufferWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

type NetworkWriter struct{}

func (nw *NetworkWriter) Write(p []byte) (int, error) {
	return len(p), nil
}
`
	ioutil.WriteFile(testFile, []byte(content), 0644)

	ia := NewInterfaceAnalyzer([]string{testFile})
	analysis, err := ia.FindImplementations("Writer")

	if err != nil {
		t.Fatalf("FindImplementations error: %v", err)
	}

	// Should find 3 implementations
	if len(analysis.Implementations) != 3 {
		t.Errorf("expected 3 implementations, got %d", len(analysis.Implementations))
	}

	// Verify all are found
	names := make(map[string]bool)
	for _, impl := range analysis.Implementations {
		names[impl.TypeName] = true
	}

	expected := []string{"FileWriter", "BufferWriter", "NetworkWriter"}
	for _, exp := range expected {
		if !names[exp] {
			t.Errorf("missing implementation: %s", exp)
		}
	}
}

func TestInterfaceInfo(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

type Reader interface {
	Read(p []byte) (n int, err error)
	Close() error
}
`
	ioutil.WriteFile(testFile, []byte(content), 0644)

	ia := NewInterfaceAnalyzer([]string{testFile})
	analysis, err := ia.FindImplementations("Reader")

	if err != nil {
		t.Fatalf("FindImplementations error: %v", err)
	}

	// Check interface info
	if analysis.Interface.Name != "Reader" {
		t.Errorf("expected interface name Reader, got %s", analysis.Interface.Name)
	}

	if len(analysis.Interface.Methods) != 2 {
		t.Errorf("expected 2 interface methods, got %d", len(analysis.Interface.Methods))
	}

	// Verify method names
	methodNames := make(map[string]bool)
	for _, method := range analysis.Interface.Methods {
		methodNames[method.Name] = true
	}

	if !methodNames["Read"] || !methodNames["Close"] {
		t.Errorf("expected Read and Close methods")
	}
}

func TestPartialImplementation(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

type ReadWriter interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
}

type PartialRW struct{}

func (p *PartialRW) Read(b []byte) (int, error) {
	return 0, nil
}
`
	ioutil.WriteFile(testFile, []byte(content), 0644)

	ia := NewInterfaceAnalyzer([]string{testFile})
	analysis, err := ia.FindImplementations("ReadWriter")

	if err != nil {
		t.Fatalf("FindImplementations error: %v", err)
	}

	// Should find partial implementation
	if len(analysis.PartialImplements) != 1 {
		t.Errorf("expected 1 partial implementation, got %d", len(analysis.PartialImplements))
	}

	// Check missing methods
	partial := analysis.PartialImplements[0]
	if len(partial.MissingMethods) != 1 || partial.MissingMethods[0] != "Write" {
		t.Errorf("expected Write to be missing, got %v", partial.MissingMethods)
	}
}

func TestInterfaceInMultipleFiles(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer os.RemoveAll(tmpDir)

	// File 1: Interface definition
	file1 := filepath.Join(tmpDir, "interface.go")
	content1 := `package main

type Reader interface {
	Read(p []byte) (n int, err error)
}
`
	ioutil.WriteFile(file1, []byte(content1), 0644)

	// File 2: First implementation
	file2 := filepath.Join(tmpDir, "impl1.go")
	content2 := `package main

type BufferReader struct{}

func (br *BufferReader) Read(p []byte) (int, error) {
	return len(p), nil
}
`
	ioutil.WriteFile(file2, []byte(content2), 0644)

	// File 3: Second implementation
	file3 := filepath.Join(tmpDir, "impl2.go")
	content3 := `package main

type FileReader struct{}

func (fr *FileReader) Read(p []byte) (int, error) {
	return len(p), nil
}
`
	ioutil.WriteFile(file3, []byte(content3), 0644)

	ia := NewInterfaceAnalyzer([]string{file1, file2, file3})
	analysis, err := ia.FindImplementations("Reader")

	if err != nil {
		t.Fatalf("FindImplementations error: %v", err)
	}

	// Should find both implementations across files
	if len(analysis.Implementations) != 2 {
		t.Errorf("expected 2 implementations, got %d", len(analysis.Implementations))
	}

	// Verify they're from different files
	files := make(map[string]bool)
	for _, impl := range analysis.Implementations {
		files[impl.File] = true
	}

	if len(files) != 2 {
		t.Errorf("expected implementations from 2 files, got %d", len(files))
	}
}
