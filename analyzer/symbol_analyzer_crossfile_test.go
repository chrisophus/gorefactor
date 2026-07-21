package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCrossFileAnalysis(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// File 1: Function definition
	file1 := filepath.Join(tmpDir, "validators.go")
	content1 := `package main

func ValidateEmail(email string) bool {
	return len(email) > 0
}
`
	if err := os.WriteFile(file1, []byte(content1), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// File 2: Function use
	file2 := filepath.Join(tmpDir, "service.go")
	content2 := `package main

func ProcessUser(user User) {
	if ValidateEmail(user.Email) {
		fmt.Println("Valid")
	}
}
`
	if err := os.WriteFile(file2, []byte(content2), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// File 3: Another use
	file3 := filepath.Join(tmpDir, "handler.go")
	content3 := `package main

func HandleRequest(req Request) {
	valid := ValidateEmail(req.Email)
	if !valid {
		return
	}
}
`
	if err := os.WriteFile(file3, []byte(content3), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	analyzer := NewUseAnalyzer([]string{file1, file2, file3})
	query := SymbolQuery{Name: "ValidateEmail"}

	uses, err := analyzer.FindAllUses(query)
	if err != nil {
		t.Fatalf("FindAllUses error: %v", err)
	}

	// Should find definition in file1 and calls in file2 and file3
	callUses := filterUsesByContext(uses, UsageCall)
	if len(callUses) != 2 {
		t.Errorf("expected 2 calls, got %d", len(callUses))
	}

	// Verify calls are in different files
	files := make(map[string]bool)
	for _, use := range callUses {
		files[use.File] = true
	}

	if len(files) != 2 {
		t.Errorf("expected calls in 2 files, got %d", len(files))
	}
}

func TestMultipleMethodShadowing(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

type TypeA struct{}

func (a *TypeA) Process() error {
	return nil
}

type TypeB struct{}

func (b *TypeB) Process() error {
	return nil
}

func UseA(a *TypeA) {
	a.Process()
}

func UseB(b *TypeB) {
	b.Process()
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	analyzer := NewUseAnalyzer([]string{testFile})

	// Query for TypeA.Process
	query := SymbolQuery{Name: "Process", Receiver: "*TypeA"}
	uses, err := analyzer.FindAllUses(query)
	if err != nil {
		t.Fatalf("FindAllUses error: %v", err)
	}

	// Should find only TypeA.Process calls, not TypeB.Process
	callUses := filterUsesByContext(uses, UsageCall)
	for _, use := range callUses {
		if use.Receiver != "" && !contains(use.Receiver, "TypeA") && !contains(use.Receiver, "*TypeA") {
			t.Errorf("unexpected method receiver: %s", use.Receiver)
		}
	}
}

func TestMethodDefinitionCapture(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

type Service struct {
	name string
}

func (s *Service) GetName() string {
	return s.name
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	analyzer := NewUseAnalyzer([]string{testFile})

	def, err := analyzer.FindSymbolDefinition(SymbolQuery{Name: "GetName"})
	if err != nil {
		t.Fatalf("FindSymbolDefinition error: %v", err)
	}

	if def == nil {
		t.Fatal("definition should not be nil")
	}

	if def.Type != TypeMethod {
		t.Errorf("expected Method, got %v", def.Type)
	}

	if def.Receiver != "*Service" && def.Receiver != "Service" {
		t.Errorf("unexpected receiver: %s", def.Receiver)
	}
}
