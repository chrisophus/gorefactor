package analyzer

import "testing"

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
