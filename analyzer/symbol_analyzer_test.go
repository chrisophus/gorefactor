package analyzer

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestFindAllUsesFunction(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer os.RemoveAll(tmpDir)

	// Create a test file with function definition and uses
	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

func ValidateEmail(email string) bool {
	return len(email) > 0
}

func ProcessUser(user User) {
	if ValidateEmail(user.Email) {
		fmt.Println("Valid")
	}
}

func main() {
	valid := ValidateEmail("test@example.com")
	if !valid {
		ProcessUser(user)
	}
}
`
	ioutil.WriteFile(testFile, []byte(content), 0644)

	analyzer := NewUseAnalyzer([]string{testFile})
	query := SymbolQuery{Name: "ValidateEmail"}

	uses, err := analyzer.FindAllUses(query)
	if err != nil {
		t.Fatalf("FindAllUses error: %v", err)
	}

	// Should find 2 calls to ValidateEmail
	callUses := FilterUsesByContext(uses, UsageCall)
	if len(callUses) != 2 {
		t.Errorf("expected 2 calls, got %d", len(callUses))
	}

	// All should be UsageCall context
	for _, use := range callUses {
		if use.Context != UsageCall {
			t.Errorf("expected UsageCall, got %v", use.Context)
		}
	}
}

func TestFindAllUsesMethod(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

type Validator struct {
	Name string
}

func (v *Validator) Validate(email string) bool {
	return len(email) > 0
}

func ProcessUser() {
	v := &Validator{}
	if v.Validate("test@example.com") {
		fmt.Println("Valid")
	}
}

type OtherValidator struct{}

func (o *OtherValidator) Validate(data string) error {
	return nil
}

func Test() {
	ov := &OtherValidator{}
	ov.Validate("data")
}
`
	ioutil.WriteFile(testFile, []byte(content), 0644)

	analyzer := NewUseAnalyzer([]string{testFile})

	// Find all calls to any Validate method (without specifying receiver, since type
	// inference from AST alone is complex). We can filter by method name.
	query := SymbolQuery{Name: "Validate"}
	uses, err := analyzer.FindAllUses(query)
	if err != nil {
		t.Fatalf("FindAllUses error: %v", err)
	}

	// Should find both the definition and calls to Validate methods
	callUses := FilterUsesByContext(uses, UsageCall)
	if len(callUses) < 2 {
		t.Errorf("expected at least 2 calls to Validate, got %d", len(callUses))
	}

	// All should be method calls (have receiver context)
	for _, use := range callUses {
		if use.Type != TypeMethod {
			t.Errorf("expected Method type, got %v", use.Type)
		}
	}
}

func TestFindSymbolDefinition(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

func ValidateEmail(email string) bool {
	return len(email) > 0
}

type Validator struct{}

func (v *Validator) Check(s string) bool {
	return len(s) > 0
}
`
	ioutil.WriteFile(testFile, []byte(content), 0644)

	analyzer := NewUseAnalyzer([]string{testFile})

	// Find function definition
	query := SymbolQuery{Name: "ValidateEmail"}
	def, err := analyzer.FindSymbolDefinition(query)
	if err != nil {
		t.Fatalf("FindSymbolDefinition error: %v", err)
	}

	if def == nil {
		t.Fatal("definition should not be nil")
	}

	if def.Name != "ValidateEmail" {
		t.Errorf("expected name ValidateEmail, got %s", def.Name)
	}

	if def.Type != TypeFunction {
		t.Errorf("expected type Function, got %v", def.Type)
	}

	if !def.IsExported {
		t.Errorf("ValidateEmail should be exported")
	}

	// Find method definition
	methodQuery := SymbolQuery{Name: "Check"}
	methodDef, err := analyzer.FindSymbolDefinition(methodQuery)
	if err != nil {
		t.Fatalf("FindSymbolDefinition for method error: %v", err)
	}

	if methodDef == nil {
		t.Fatal("method definition should not be nil")
	}

	if methodDef.Type != TypeMethod {
		t.Errorf("expected type Method, got %v", methodDef.Type)
	}
}

func TestGetSymbolType(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

func DoSomething() {}

type MyStruct struct{}

type MyInterface interface {
	Read() error
}

var globalVar int
`
	ioutil.WriteFile(testFile, []byte(content), 0644)

	analyzer := NewUseAnalyzer([]string{testFile})

	tests := []struct {
		name     string
		expected SymbolType
	}{
		{"DoSomething", TypeFunction},
		{"MyStruct", TypeStruct},
		{"MyInterface", TypeInterface},
		{"globalVar", TypeVariable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			symType, err := analyzer.GetSymbolType(test.name)
			if err != nil {
				t.Fatalf("GetSymbolType error: %v", err)
			}

			if symType != test.expected {
				t.Errorf("expected %v, got %v", test.expected, symType)
			}
		})
	}
}

func TestFilterUsesByContext(t *testing.T) {
	uses := []SymbolUse{
		{SymbolName: "x", Context: UsageCall},
		{SymbolName: "x", Context: UsageRead},
		{SymbolName: "x", Context: UsageRead},
		{SymbolName: "x", Context: UsageWrite},
		{SymbolName: "x", Context: UsageDefine},
		{SymbolName: "x", Context: UsageReturn},
	}

	tests := []struct {
		name     string
		contexts []UseContext
		expected int
	}{
		{
			name:     "filter calls only",
			contexts: []UseContext{UsageCall},
			expected: 1,
		},
		{
			name:     "filter reads",
			contexts: []UseContext{UsageRead},
			expected: 2,
		},
		{
			name:     "filter multiple",
			contexts: []UseContext{UsageRead, UsageDefine},
			expected: 3,
		},
		{
			name:     "filter none",
			contexts: []UseContext{},
			expected: 6, // Returns all
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := FilterUsesByContext(uses, test.contexts...)
			if len(result) != test.expected {
				t.Errorf("expected %d results, got %d", test.expected, len(result))
			}
		})
	}
}

func TestCrossFileAnalysis(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer os.RemoveAll(tmpDir)

	// File 1: Function definition
	file1 := filepath.Join(tmpDir, "validators.go")
	content1 := `package main

func ValidateEmail(email string) bool {
	return len(email) > 0
}
`
	ioutil.WriteFile(file1, []byte(content1), 0644)

	// File 2: Function use
	file2 := filepath.Join(tmpDir, "service.go")
	content2 := `package main

func ProcessUser(user User) {
	if ValidateEmail(user.Email) {
		fmt.Println("Valid")
	}
}
`
	ioutil.WriteFile(file2, []byte(content2), 0644)

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
	ioutil.WriteFile(file3, []byte(content3), 0644)

	analyzer := NewUseAnalyzer([]string{file1, file2, file3})
	query := SymbolQuery{Name: "ValidateEmail"}

	uses, err := analyzer.FindAllUses(query)
	if err != nil {
		t.Fatalf("FindAllUses error: %v", err)
	}

	// Should find definition in file1 and calls in file2 and file3
	callUses := FilterUsesByContext(uses, UsageCall)
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
	defer os.RemoveAll(tmpDir)

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
	ioutil.WriteFile(testFile, []byte(content), 0644)

	analyzer := NewUseAnalyzer([]string{testFile})

	// Query for TypeA.Process
	query := SymbolQuery{Name: "Process", Receiver: "*TypeA"}
	uses, err := analyzer.FindAllUses(query)
	if err != nil {
		t.Fatalf("FindAllUses error: %v", err)
	}

	// Should find only TypeA.Process calls, not TypeB.Process
	callUses := FilterUsesByContext(uses, UsageCall)
	for _, use := range callUses {
		if use.Receiver != "" && !contains(use.Receiver, "TypeA") && !contains(use.Receiver, "*TypeA") {
			t.Errorf("unexpected method receiver: %s", use.Receiver)
		}
	}
}

func TestMethodDefinitionCapture(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

type Service struct {
	name string
}

func (s *Service) GetName() string {
	return s.name
}
`
	ioutil.WriteFile(testFile, []byte(content), 0644)

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

// Helper function to create temporary test directory
func createTempTestDir(t *testing.T) string {
	tmpDir, err := ioutil.TempDir("", "gorefactor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	return tmpDir
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && s[len(s)-len(substr):] == substr || len(s) > len(substr) && s[:len(substr)] == substr)
}
