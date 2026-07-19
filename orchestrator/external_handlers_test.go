package orchestrator

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestBuiltinOperationTypes_MatchDispatch pins builtinOperationTypes to the
// real dispatch switch: every listed type must NOT produce the
// "unknown operation type" error, and an unlisted type must. This is what
// lets KnownOperationTypes be trusted as "what can a plan contain".
func TestBuiltinOperationTypes_MatchDispatch(t *testing.T) {
	dir := t.TempDir()
	for _, opType := range builtinOperationTypes {
		o := NewOrchestrator()
		op := &RefactoringOperation{
			Type: opType,
			File: filepath.Join(dir, "does-not-exist.go"),
		}
		result := &OperationResult{Operation: op}
		err := o.dispatchOperation(op, nil, result)
		if err != nil && strings.Contains(err.Error(), "unknown operation type") {
			t.Errorf("type %q is listed as builtin but dispatch does not know it", opType)
		}
	}

	o := NewOrchestrator()
	op := &RefactoringOperation{Type: "definitely_not_an_op", File: filepath.Join(dir, "x.go")}
	err := o.dispatchOperation(op, nil, &OperationResult{Operation: op})
	if err == nil || !strings.Contains(err.Error(), "unknown operation type") {
		t.Errorf("bogus op type should be unknown, got err=%v", err)
	}
}

// TestRegisterExternalHandler_Dispatches pins the external-handler path: a
// registered type dispatches through the handler and its changes land on the
// result.
func TestRegisterExternalHandler_Dispatches(t *testing.T) {
	called := false
	RegisterExternalHandler("test_external_op_fixture", func(op *RefactoringOperation, target *TargetLocation) ([]*CodeChange, error) {
		called = true
		return []*CodeChange{{Type: "test_external_op_fixture", File: op.File}}, nil
	})
	o := NewOrchestrator()
	op := &RefactoringOperation{Type: "test_external_op_fixture", File: "whatever.go"}
	result := &OperationResult{Operation: op}
	if err := o.dispatchOperation(op, nil, result); err != nil {
		t.Fatalf("external dispatch failed: %v", err)
	}
	if !called {
		t.Fatal("external handler was not invoked")
	}
	if len(result.Changes) != 1 {
		t.Fatalf("handler changes not appended: %+v", result.Changes)
	}
	found := false
	for _, kt := range KnownOperationTypes() {
		if kt == "test_external_op_fixture" {
			found = true
		}
	}
	if !found {
		t.Error("registered external type missing from KnownOperationTypes")
	}
}
