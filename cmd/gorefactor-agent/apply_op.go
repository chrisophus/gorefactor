package main

import (
	"fmt"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

// applyOp builds one orchestrator operation from tool args, executes it
// deterministically, and gofmt's the touched file. Returns a tight
// success or structured-error string the model can react to.
func applyOp(kind string, a map[string]any, cfg Config) string {
	str := func(k string) string { s, _ := a[k].(string); return strings.TrimSpace(s) }
	op := &orchestrator.RefactoringOperation{Type: kind, Description: kind, File: str("file")}
	tgt := &orchestrator.TargetSpecification{}
	params := map[string]any{}

	switch kind {
	case "rename_declaration":
		if fn := str("function"); fn != "" {
			tgt.FunctionName = fn
		}
		if m := str("method"); m != "" {
			tgt.MethodName = m
		}
		if t := str("type"); t != "" {
			tgt.TypeName = t
		}
		op.Target = tgt
		params["newName"] = str("new_name")
	case "replace_code":
		params["location"] = map[string]any{"functionName": str("function")}
		params["codePattern"] = str("code_pattern")
		params["replacementCode"] = str("replacement_code")
	case "insert_code":
		loc := map[string]any{"type": str("location_type")}
		if anc := str("anchor_function"); anc != "" {
			loc["functionName"] = anc
		}
		params["location"] = loc
		params["codeSnippet"] = str("code_snippet")
	case "create_file":
		params["codeSnippet"] = str("code_snippet")
	case "move_method":
		tgt.MethodName = str("method")
		tgt.ReceiverType = str("receiver_type")
		op.Target = tgt
		params["newFile"] = str("new_file")
	case "delete_declaration":
		if fn := str("function"); fn != "" {
			tgt.FunctionName = fn
		}
		if m := str("method"); m != "" {
			tgt.MethodName = m
		}
		if t := str("type"); t != "" {
			tgt.TypeName = t
		}
		op.Target = tgt
	case "remove_code_block":
		params["codePattern"] = str("code_pattern")
	}
	if len(params) > 0 {
		op.Parameters = params
	}

	o := orchestrator.NewOrchestrator()
	res, err := o.ExecuteOperations([]*orchestrator.RefactoringOperation{op})
	if err != nil {
		return "ERROR: " + trim(err.Error(), 400)
	}
	if res == nil || !res.Success {
		return "FAILED: " + trim(execErrors(res, nil), 600)
	}
	if op.File != "" {
		_, _ = runIn(".", "gofmt", "-w", op.File)
	}
	return fmt.Sprintf("applied %s on %s", kind, op.File)
}
