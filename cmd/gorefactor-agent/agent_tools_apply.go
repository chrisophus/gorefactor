package main

import (
	"fmt"
	"strings"
)

// applySplitFile runs `gorefactor split <file>` and returns a compact result.
func applySplitFile(file string) string {
	if file == "" {
		return "ERROR: 'file' required"
	}
	out, err := runIn(".", gorefactorBin(), "split", file)
	if err != nil {
		return "ERROR splitting file: " + trim(out, 400)
	}
	return "split " + file + ": " + trim(out, 300)
}

// applyWrapErrors runs `gorefactor wrap-errors <file> <function>`.
func applyWrapErrors(file, function string) string {
	if file == "" || function == "" {
		return "ERROR: 'file' and 'function' are required"
	}
	out, err := runIn(".", gorefactorBin(), "wrap-errors", file, function)
	if err != nil {
		return "ERROR wrapping errors: " + trim(out, 400)
	}
	return trim(out, 400)
}

// applySetDoc runs `gorefactor set-doc <file> <decl> -` with the doc text piped in.
func applySetDoc(file, decl, doc string) string {
	if file == "" || decl == "" || doc == "" {
		return "ERROR: 'file', 'declaration', and 'doc' are all required"
	}
	// set-doc reads content from stdin when the last arg is "-"
	out, err := runInWithStdin(".", doc, gorefactorBin(), "set-doc", file, decl, "-")
	if err != nil {
		return "ERROR setting doc: " + trim(out, 400)
	}
	return fmt.Sprintf("set doc on %s in %s", decl, file)
}

// applyExtractMethod runs `gorefactor extract <file> <start> <end> <name>` and
// returns a compact result string the model can react to.
func applyExtractMethod(file, start, end, name string) string {
	if file == "" || start == "" || end == "" || name == "" {
		return "ERROR: file, start_line, end_line, and new_function_name are all required"
	}
	out, err := runIn(".", gorefactorBin(), "extract", file, start, end, name)
	if err != nil {
		return "ERROR extracting method: " + trim(out, 400)
	}
	return fmt.Sprintf("extracted lines %s-%s into %s in %s", start, end, name, file)
}

// applyChangeSignature runs `gorefactor change-signature` in one of three
// modes (add/remove/rename a parameter), updating every call site in one op.
// It maps a flat tool schema (mode + fields) onto the CLI's mutually
// exclusive flags and validates the combination, returning a structured
// ERROR on a mismatch so the failure corpus captures schema confusion
// rather than a silent bad edit.
func applyChangeSignature(a map[string]any) string {
	str := func(k string) string { s, _ := a[k].(string); return strings.TrimSpace(s) }
	file, symbol, mode := str("file"), str("symbol"), str("mode")
	if file == "" || symbol == "" {
		return "ERROR: 'file' and 'symbol' are required"
	}
	args := []string{"change-signature", file, symbol}
	switch mode {
	case "add_param":
		spec := str("param_spec")
		if len(strings.Fields(spec)) < 2 {
			return `ERROR: add_param needs param_spec "name type" (e.g. "ctx context.Context")`
		}
		args = append(args, "--add-param", spec)
		if _, ok := a["position"]; ok {
			args = append(args, "--position", fmt.Sprintf("%d", intArg(a, "position")))
		}
		if cv := str("call_value"); cv != "" {
			args = append(args, "--call-value", cv)
		}
	case "remove_param":
		p := str("param")
		if p == "" {
			return "ERROR: remove_param needs 'param' (a parameter name or 0-based index)"
		}
		args = append(args, "--remove-param", p)
	case "rename_param":
		oldName, newName := str("old_name"), str("new_name")
		if oldName == "" || newName == "" {
			return "ERROR: rename_param needs 'old_name' and 'new_name'"
		}
		args = append(args, "--rename-param", oldName+"="+newName)
	default:
		return "ERROR: 'mode' must be one of add_param | remove_param | rename_param"
	}
	out, err := runIn(".", gorefactorBin(), args...)
	if err != nil {
		return "ERROR change-signature: " + trim(out, 400)
	}
	return trim(out, 400)
}
