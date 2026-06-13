package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConditionsFixture(t *testing.T) string {
	t.Helper()
	testFile := filepath.Join(t.TempDir(), "fixture.go")
	code := `package main

import "fmt"

func simpleFunc() {
	fmt.Println("simple")
}

func complexFunc(items []int) (int, error) {
	total := 0
	for _, item := range items {
		if item < 0 {
			return 0, fmt.Errorf("negative item")
		}
		if item > 0 && item < 100 {
			total += item
		}
	}
	if err := validate(total); err != nil {
		return 0, err
	}
	return total, nil
}

func validate(n int) error {
	return nil
}

type Thing struct{}

func (t *Thing) Process() {
	fmt.Println("process")
}
`
	if err := os.WriteFile(testFile, []byte(code), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return testFile
}

func TestEvaluateCondition_Complexity(t *testing.T) {
	orch := NewOrchestrator()
	testFile := writeConditionsFixture(t)

	cases := []struct {
		name     string
		target   string
		property string
		operator string
		value    interface{}
		want     bool
	}{
		{"control structures gte met", "complexFunc", "controlStructures", "gte", float64(2), true},
		{"control structures gte unmet", "simpleFunc", "controlStructures", "gte", float64(1), false},
		{"statement count gt", "complexFunc", "statementCount", "gt", float64(3), true},
		{"statement count lt unmet", "complexFunc", "statementCount", "lt", float64(2), false},
		{"return count gte", "complexFunc", "returnCount", "gte", float64(3), true},
		{"error handling paths", "complexFunc", "errorHandlingPaths", "eq", float64(1), true},
		{"logical operators eq", "complexFunc", "logicalOperators", "eq", float64(1), true},
		{"nesting depth gte", "complexFunc", "maxNestingDepth", "gte", float64(2), true},
		{"default operator is gte", "complexFunc", "controlStructures", "", float64(2), true},
		{"ne operator", "simpleFunc", "returnCount", "ne", float64(1), true},
		{"lte operator", "simpleFunc", "statementCount", "lte", float64(5), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := &RefactoringOperation{
				Type:   "move_method",
				File:   testFile,
				Target: &TargetSpecification{FunctionName: tc.target},
			}
			cond := &Condition{Type: "complexity", Property: tc.property, Operator: tc.operator, Value: tc.value}
			got, err := orch.evaluateCondition(cond, op)
			if err != nil {
				t.Fatalf("evaluateCondition failed: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEvaluateCondition_Errors(t *testing.T) {
	orch := NewOrchestrator()
	testFile := writeConditionsFixture(t)

	cases := []struct {
		name    string
		op      *RefactoringOperation
		cond    *Condition
		wantErr string
	}{
		{
			"unknown type",
			&RefactoringOperation{File: testFile},
			&Condition{Type: "phase_of_moon", Property: "x", Value: 1.0},
			"unknown condition type",
		},
		{
			"missing type",
			&RefactoringOperation{File: testFile},
			&Condition{Property: "x", Value: 1.0},
			"condition type is required",
		},
		{
			"unknown complexity property",
			&RefactoringOperation{File: testFile, Target: &TargetSpecification{FunctionName: "simpleFunc"}},
			&Condition{Type: "complexity", Property: "moonPhase", Value: 1.0},
			"unknown complexity property",
		},
		{
			"non-numeric complexity value",
			&RefactoringOperation{File: testFile, Target: &TargetSpecification{FunctionName: "simpleFunc"}},
			&Condition{Type: "complexity", Property: "returnCount", Value: "many"},
			"must be numeric",
		},
		{
			"unknown operator",
			&RefactoringOperation{File: testFile, Target: &TargetSpecification{FunctionName: "simpleFunc"}},
			&Condition{Type: "complexity", Property: "returnCount", Operator: "approximately", Value: 1.0},
			"unknown operator",
		},
		{
			"contains on numeric",
			&RefactoringOperation{File: testFile, Target: &TargetSpecification{FunctionName: "simpleFunc"}},
			&Condition{Type: "complexity", Property: "returnCount", Operator: "contains", Value: 1.0},
			"not applicable to numeric",
		},
		{
			"complexity without target",
			&RefactoringOperation{File: testFile},
			&Condition{Type: "complexity", Property: "returnCount", Value: 1.0},
			"requires an operation target",
		},
		{
			"complexity with unresolvable target",
			&RefactoringOperation{File: testFile, Target: &TargetSpecification{FunctionName: "noSuchFunc"}},
			&Condition{Type: "complexity", Property: "returnCount", Value: 1.0},
			"cannot resolve target",
		},
		{
			"function_exists without property",
			&RefactoringOperation{File: testFile},
			&Condition{Type: "function_exists", Value: true},
			"requires a function name",
		},
		{
			"file_exists without property",
			&RefactoringOperation{File: testFile},
			&Condition{Type: "file_exists", Value: true},
			"requires a file path",
		},
		{
			"file_exists non-bool value",
			&RefactoringOperation{File: testFile},
			&Condition{Type: "file_exists", Property: testFile, Value: 42.0},
			"must be a boolean",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := orch.evaluateCondition(tc.cond, tc.op)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestEvaluateCondition_FunctionExists(t *testing.T) {
	orch := NewOrchestrator()
	testFile := writeConditionsFixture(t)
	op := &RefactoringOperation{File: testFile}

	cases := []struct {
		name     string
		property string
		operator string
		value    interface{}
		want     bool
	}{
		{"existing function", "complexFunc", "eq", true, true},
		{"missing function", "noSuchFunc", "eq", true, false},
		{"missing function expected missing", "noSuchFunc", "eq", false, true},
		{"existing method with receiver", "Thing:Process", "eq", true, true},
		{"method with wrong receiver", "Widget:Process", "eq", true, false},
		{"ne operator", "noSuchFunc", "ne", true, true},
		{"default operator eq", "complexFunc", "", true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cond := &Condition{Type: "function_exists", Property: tc.property, Operator: tc.operator, Value: tc.value}
			got, err := orch.evaluateCondition(cond, op)
			if err != nil {
				t.Fatalf("evaluateCondition failed: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEvaluateCondition_FileExists(t *testing.T) {
	orch := NewOrchestrator()
	testFile := writeConditionsFixture(t)
	op := &RefactoringOperation{File: testFile}

	got, err := orch.evaluateCondition(&Condition{Type: "file_exists", Property: testFile, Value: true}, op)
	if err != nil || !got {
		t.Errorf("expected existing file condition to pass, got=%v err=%v", got, err)
	}
	got, err = orch.evaluateCondition(&Condition{Type: "file_exists", Property: testFile + ".missing", Value: false}, op)
	if err != nil || !got {
		t.Errorf("expected missing file condition (value=false) to pass, got=%v err=%v", got, err)
	}
}

// TestExecuteOperation_ConditionEvaluationErrorFails ensures an unevaluable
// condition fails the operation instead of silently passing.
func TestExecuteOperation_ConditionEvaluationErrorFails(t *testing.T) {
	orch := NewOrchestrator()
	testFile := writeConditionsFixture(t)

	op := &RefactoringOperation{
		Type:   "move_method",
		File:   testFile,
		Target: &TargetSpecification{FunctionName: "simpleFunc"},
		Parameters: map[string]interface{}{
			"newFile": filepath.Join(filepath.Dir(testFile), "dest.go"),
		},
		Conditions: []*Condition{
			{Type: "not_a_real_condition", Property: "x", Value: 1.0},
		},
	}
	result := orch.executeOperation(op)
	if result.Success {
		t.Fatal("expected operation to fail when a condition cannot be evaluated")
	}
	if !strings.Contains(result.Error, "condition evaluation failed") {
		t.Errorf("expected condition evaluation error, got: %q", result.Error)
	}
	// The move must not have happened.
	if _, err := os.Stat(filepath.Join(filepath.Dir(testFile), "dest.go")); !os.IsNotExist(err) {
		t.Error("operation mutated files despite condition evaluation error")
	}
}

// TestExecuteOperation_UnmetConditionSkips ensures an unmet (but evaluable)
// condition blocks execution with the documented message.
func TestExecuteOperation_UnmetConditionSkips(t *testing.T) {
	orch := NewOrchestrator()
	testFile := writeConditionsFixture(t)

	op := &RefactoringOperation{
		Type:   "move_method",
		File:   testFile,
		Target: &TargetSpecification{FunctionName: "simpleFunc"},
		Parameters: map[string]interface{}{
			"newFile": filepath.Join(filepath.Dir(testFile), "dest.go"),
		},
		Conditions: []*Condition{
			{Type: "complexity", Property: "controlStructures", Operator: "gte", Value: 5.0},
		},
	}
	result := orch.executeOperation(op)
	if result.Success {
		t.Fatal("expected operation with unmet condition not to run")
	}
	if result.Message != "Conditions not met" {
		t.Errorf("expected 'Conditions not met', got %q", result.Message)
	}
	if result.Applied {
		t.Error("operation must not be applied when conditions are unmet")
	}
}
