package main

import (
	"fmt"
	"strconv"

	"github.com/chrisophus/gorefactor/orchestrator"
)

// Plan-level bridges for engines that live in this package (harness-integrity
// follow-up): extract_method needs the go/packages type inference in
// cmd_extract.go and inline_method needs the inliner in cmd_inline_run.go, so
// the orchestrator cannot own them. Before this bridge existed the tool's own
// generated templates failed with "unknown operation type: extract_method".
// Each bridge delegates to the CLI command function, so the op is journaled
// and gated exactly like the direct command.

func init() {
	orchestrator.RegisterExternalHandler("extract_method", execExtractMethodOp)
	orchestrator.RegisterExternalHandler("inline_method", execInlineMethodOp)
}

// execExtractMethodOp runs a plan-level extract_method: the block is chosen
// by explicit startLine/endLine parameters, an explicit target line range, or
// the semantically resolved target's range — in that priority order.
func execExtractMethodOp(op *orchestrator.RefactoringOperation, target *orchestrator.TargetLocation) ([]*orchestrator.CodeChange, error) {
	name := planParamString(op, "methodName", "newFunctionName")
	if name == "" {
		return nil, fmt.Errorf("extract_method requires a methodName parameter")
	}
	start, end := planParamInt(op, "startLine"), planParamInt(op, "endLine")
	if start == 0 && op.Target != nil && op.Target.StartLine != nil {
		start = *op.Target.StartLine
	}
	if end == 0 && op.Target != nil && op.Target.EndLine != nil {
		end = *op.Target.EndLine
	}
	if (start == 0 || end == 0) && target != nil {
		start, end = target.StartLine, target.EndLine
	}
	if start == 0 || end == 0 || end < start {
		return nil, fmt.Errorf("extract_method: cannot determine a line range (set startLine/endLine parameters or a resolvable target)")
	}
	if err := extractCommand([]string{op.File, strconv.Itoa(start), strconv.Itoa(end), name}); err != nil {
		return nil, err
	}
	return []*orchestrator.CodeChange{{
		Type:        "extract_method",
		File:        op.File,
		StartLine:   start,
		EndLine:     end,
		Description: fmt.Sprintf("Extracted lines %d-%d into %s", start, end, name),
	}}, nil
}

// execInlineMethodOp runs a plan-level inline_method: the function named by
// the methodName parameter (or the target spec) is inlined into its callers
// and deleted, with the inliner's own refusal rules unchanged.
func execInlineMethodOp(op *orchestrator.RefactoringOperation, target *orchestrator.TargetLocation) ([]*orchestrator.CodeChange, error) {
	name := planParamString(op, "methodName", "functionName")
	if name == "" && op.Target != nil {
		name = op.Target.FunctionName
	}
	if name == "" && target != nil {
		name = target.Function
	}
	if name == "" {
		return nil, fmt.Errorf("inline_method requires a methodName parameter or function target")
	}
	if err := inlineCommand([]string{op.File, name}); err != nil {
		return nil, err
	}
	return []*orchestrator.CodeChange{{
		Type:        "inline_method",
		File:        op.File,
		Description: fmt.Sprintf("Inlined %s into its call sites", name),
	}}, nil
}

// planParamString returns the first non-empty string parameter among keys.
func planParamString(op *orchestrator.RefactoringOperation, keys ...string) string {
	for _, k := range keys {
		if s, ok := op.Parameters[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// planParamInt reads an integer parameter that may arrive as a JSON number
// (float64) or a string.
func planParamInt(op *orchestrator.RefactoringOperation, key string) int {
	switch v := op.Parameters[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}
