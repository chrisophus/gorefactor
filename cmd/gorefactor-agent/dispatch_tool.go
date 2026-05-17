package main

import (
	"encoding/json"
	"strings"
)

// dispatchTool routes one tool call. Sense tools are read-only; mutate
// tools are single deterministic orchestrator ops; finish runs the
// authoritative gate; punt is terminal.
func dispatchTool(call toolCall, cfg Config, gateFails *int) (string, toolStatus) {
	var a map[string]any
	if call.Function.Arguments != "" {
		_ = json.Unmarshal([]byte(call.Function.Arguments), &a)
	}
	if a == nil {
		a = map[string]any{}
	}
	str := func(k string) string { s, _ := a[k].(string); return strings.TrimSpace(s) }

	switch call.Function.Name {
	case "punt":
		r := str("reason")
		if r == "" {
			r = "model punted without a reason"
		}
		return r, stPunt

	case "finish":
		ok, out := runGate(".")
		if ok {
			return "gate green", stSuccess
		}
		*gateFails++
		return "gate FAILED (not done). Fix and call finish again:\n" + trim(out, 1200), stContinue

	case "run_gate":
		ok, out := runGate(".")
		if ok {
			return "gate green", stContinue
		}
		return "gate red:\n" + trim(out, 1000), stContinue

	case "list_symbols":
		return senseListSymbols(str("file")), stContinue

	case "read_excerpt":
		return senseReadExcerpt(str("file"), a), stContinue

	case "analyze_file_size":
		return senseFileSize(str("file")), stContinue

	case "find_references":
		return senseFindRefs(str("symbol")), stContinue

	case "rename_declaration", "replace_code", "insert_code",
		"create_file", "move_method", "delete_declaration", "remove_code_block":
		return applyOp(call.Function.Name, a, cfg), stContinue

	default:
		return "unknown tool: " + call.Function.Name, stContinue
	}
}
