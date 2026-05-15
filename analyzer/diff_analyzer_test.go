package analyzer

import (
	"testing"
)

func TestNewDiffAnalyzer(t *testing.T) {
	da := NewDiffAnalyzer()
	if da == nil {
		t.Fatal("NewDiffAnalyzer() returned nil")
	}
}

func TestAnalyzeDiffString_EmptyDiff(t *testing.T) {
	da := NewDiffAnalyzer()
	analysis, err := da.AnalyzeDiffString("")
	if err != nil {
		t.Fatalf("AnalyzeDiffString() failed: %v", err)
	}

	if analysis == nil {
		t.Fatal("Analysis is nil")
	}

	if len(analysis.Files) != 0 {
		t.Errorf("Expected 0 files, got %d", len(analysis.Files))
	}

	if len(analysis.Changes) != 0 {
		t.Errorf("Expected 0 changes, got %d", len(analysis.Changes))
	}

	if analysis.Plan == nil {
		t.Fatal("Plan is nil")
	}

	if len(analysis.Plan.Operations) != 0 {
		t.Errorf("Expected 0 operations, got %d", len(analysis.Plan.Operations))
	}
}

func TestAnalyzeDiffString_SimpleFunctionAddition(t *testing.T) {
	diffContent := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -50,0 +50,4 @@
+func testFunction() {
+    fmt.Println("Hello!")
+    return true
+}`

	da := NewDiffAnalyzer()
	analysis, err := da.AnalyzeDiffString(diffContent)
	if err != nil {
		t.Fatalf("AnalyzeDiffString() failed: %v", err)
	}

	// Check files
	if len(analysis.Files) != 1 {
		t.Fatalf("Expected 1 file, got %d", len(analysis.Files))
	}

	file := analysis.Files[0]
	if file.Path != "main.go" {
		t.Errorf("Expected path 'main.go', got '%s'", file.Path)
	}

	if file.Language != "go" {
		t.Errorf("Expected language 'go', got '%s'", file.Language)
	}

	// Check hunks
	if len(file.Hunks) != 1 {
		t.Fatalf("Expected 1 hunk, got %d", len(file.Hunks))
	}

	hunk := file.Hunks[0]
	if hunk.StartLine != 50 {
		t.Errorf("Expected start line 50, got %d", hunk.StartLine)
	}

	if hunk.EndLine != 53 {
		t.Errorf("Expected end line 53, got %d", hunk.EndLine)
	}

	// Check changes
	if len(analysis.Changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(analysis.Changes))
	}

	change := analysis.Changes[0]
	if change.Type != "function_addition" {
		t.Errorf("Expected type 'function_addition', got '%s'", change.Type)
	}

	if change.File != "main.go" {
		t.Errorf("Expected file 'main.go', got '%s'", change.File)
	}

	if change.Confidence < 0.8 {
		t.Errorf("Expected confidence >= 0.8, got %f", change.Confidence)
	}

	// Check plan
	if analysis.Plan == nil {
		t.Fatal("Plan is nil")
	}

	if len(analysis.Plan.Operations) != 1 {
		t.Fatalf("Expected 1 operation, got %d", len(analysis.Plan.Operations))
	}

	operation := analysis.Plan.Operations[0]
	if operation.Type != "insert_code" {
		t.Errorf("Expected operation type 'insert_code', got '%s'", operation.Type)
	}

	// Check fallback strategy is valid
	if operation.Fallback == nil {
		t.Fatal("Fallback strategy is nil")
	}

	validFallbacks := map[string]bool{
		"skip":        true,
		"use_default": true,
	}

	if !validFallbacks[operation.Fallback.Type] {
		t.Errorf("Invalid fallback strategy: %s. Valid options are: skip, use_default", operation.Fallback.Type)
	}
}

func TestAnalyzeDiffString_MethodAddition(t *testing.T) {
	diffContent := `diff --git a/service.go b/service.go
--- a/service.go
+++ b/service.go
@@ -25,0 +25,8 @@
+func (s *UserService) ValidateUser(user *User) error {
+    if user.Name == "" {
+        return errors.New("name is required")
+    }
+    return nil
+}`

	da := NewDiffAnalyzer()
	analysis, err := da.AnalyzeDiffString(diffContent)
	if err != nil {
		t.Fatalf("AnalyzeDiffString() failed: %v", err)
	}

	if len(analysis.Changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(analysis.Changes))
	}

	change := analysis.Changes[0]
	if change.Type != "method_addition" {
		t.Errorf("Expected type 'method_addition', got '%s'", change.Type)
	}

	// Check extracted method name
	methodName, exists := change.Details["methodName"]
	if !exists {
		t.Fatal("Method name not found in details")
	}

	if methodName != "ValidateUser" {
		t.Errorf("Expected method name 'ValidateUser', got '%s'", methodName)
	}

	// Check receiver type
	receiverType, exists := change.Details["receiverType"]
	if !exists {
		t.Fatal("Receiver type not found in details")
	}

	if receiverType != "UserService" {
		t.Errorf("Expected receiver type 'UserService', got '%s'", receiverType)
	}
}

func TestAnalyzeDiffString_InterfaceAddition(t *testing.T) {
	diffContent := `diff --git a/interfaces.go b/interfaces.go
--- a/interfaces.go
+++ b/interfaces.go
@@ -10,0 +10,5 @@
+type UserValidator interface {
+    Validate(user *User) error
+    IsValid(user *User) bool
+}`

	da := NewDiffAnalyzer()
	analysis, err := da.AnalyzeDiffString(diffContent)
	if err != nil {
		t.Fatalf("AnalyzeDiffString() failed: %v", err)
	}

	if len(analysis.Changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(analysis.Changes))
	}

	change := analysis.Changes[0]
	if change.Type != "interface_addition" {
		t.Errorf("Expected type 'interface_addition', got '%s'", change.Type)
	}

	// Check extracted interface name
	interfaceName, exists := change.Details["interfaceName"]
	if !exists {
		t.Fatal("Interface name not found in details")
	}

	if interfaceName != "UserValidator" {
		t.Errorf("Expected interface name 'UserValidator', got '%s'", interfaceName)
	}
}

func TestAnalyzeDiffString_StructAddition(t *testing.T) {
	diffContent := `diff --git a/models.go b/models.go
--- a/models.go
+++ b/models.go
@@ -15,0 +15,6 @@
+type User struct {
+    ID    int
+    Name  string
+    Email string
+}`

	da := NewDiffAnalyzer()
	analysis, err := da.AnalyzeDiffString(diffContent)
	if err != nil {
		t.Fatalf("AnalyzeDiffString() failed: %v", err)
	}

	if len(analysis.Changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(analysis.Changes))
	}

	change := analysis.Changes[0]
	if change.Type != "struct_addition" {
		t.Errorf("Expected type 'struct_addition', got '%s'", change.Type)
	}

	// Check extracted struct name
	structName, exists := change.Details["structName"]
	if !exists {
		t.Fatal("Struct name not found in details")
	}

	if structName != "User" {
		t.Errorf("Expected struct name 'User', got '%s'", structName)
	}
}

func TestAnalyzeDiffString_VariableRename(t *testing.T) {
	diffContent := `diff --git a/service.go b/service.go
--- a/service.go
+++ b/service.go
@@ -45,1 +45,1 @@
-    oldName := "test"
+    newName := "test"
@@ -46,1 +46,1 @@
-    fmt.Println(oldName)
+    fmt.Println(newName)`

	da := NewDiffAnalyzer()
	analysis, err := da.AnalyzeDiffString(diffContent)
	if err != nil {
		t.Fatalf("AnalyzeDiffString() failed: %v", err)
	}

	if len(analysis.Changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(analysis.Changes))
	}

	change := analysis.Changes[0]
	if change.Type != "variable_rename" {
		t.Errorf("Expected type 'variable_rename', got '%s'", change.Type)
	}

	// Check old and new names
	oldName, exists := change.Details["oldName"]
	if !exists {
		t.Fatal("Old name not found in details")
	}

	if oldName != "oldName" {
		t.Errorf("Expected old name 'oldName', got '%s'", oldName)
	}

	newName, exists := change.Details["newName"]
	if !exists {
		t.Fatal("New name not found in details")
	}

	if newName != "newName" {
		t.Errorf("Expected new name 'newName', got '%s'", newName)
	}
}

func TestAnalyzeDiffString_MultipleChanges(t *testing.T) {
	diffContent := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -50,0 +50,4 @@
+func function1() {
+    fmt.Println("Hello!")
+}
@@ -60,0 +60,4 @@
+func function2() {
+    fmt.Println("World!")
+}`

	da := NewDiffAnalyzer()
	analysis, err := da.AnalyzeDiffString(diffContent)
	if err != nil {
		t.Fatalf("AnalyzeDiffString() failed: %v", err)
	}

	if len(analysis.Changes) != 2 {
		t.Fatalf("Expected 2 changes, got %d", len(analysis.Changes))
	}

	if len(analysis.Plan.Operations) != 2 {
		t.Fatalf("Expected 2 operations, got %d", len(analysis.Plan.Operations))
	}

	// Check that all operations have valid fallback strategies
	for i, operation := range analysis.Plan.Operations {
		if operation.Fallback == nil {
			t.Errorf("Operation %d: Fallback strategy is nil", i+1)
			continue
		}

		validFallbacks := map[string]bool{
			"skip":        true,
			"use_default": true,
		}

		if !validFallbacks[operation.Fallback.Type] {
			t.Errorf("Operation %d: Invalid fallback strategy: %s", i+1, operation.Fallback.Type)
		}
	}
}

func TestParseHunkHeader(t *testing.T) {
	da := NewDiffAnalyzer()

	testCases := []struct {
		line      string
		startLine int
		endLine   int
	}{
		{"@@ -50,4 +50,4 @@", 50, 53},
		{"@@ -1,1 +1,1 @@", 1, 1},
		{"@@ -10,0 +10,5 @@", 10, 14},
		{"@@ -25,8 +25,0 @@", 25, 24}, // This case might need special handling
	}

	for _, tc := range testCases {
		hunk := da.parseHunkHeader(tc.line)
		if hunk.StartLine != tc.startLine {
			t.Errorf("For line '%s': expected start line %d, got %d", tc.line, tc.startLine, hunk.StartLine)
		}
		if hunk.EndLine != tc.endLine {
			t.Errorf("For line '%s': expected end line %d, got %d", tc.line, tc.endLine, hunk.EndLine)
		}
	}
}

func TestDetectLanguage(t *testing.T) {
	da := NewDiffAnalyzer()

	testCases := []struct {
		path     string
		expected string
	}{
		{"main.go", "go"},
		{"service.go", "go"},
		{"file.js", "javascript"},
		{"file.ts", "javascript"},
		{"file.py", "python"},
		{"file.java", "java"},
		{"file.txt", "unknown"},
		{"file", "unknown"},
	}

	for _, tc := range testCases {
		result := da.detectLanguage(tc.path)
		if result != tc.expected {
			t.Errorf("For path '%s': expected '%s', got '%s'", tc.path, tc.expected, result)
		}
	}
}
