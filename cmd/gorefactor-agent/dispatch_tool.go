package main

import (
	"encoding/json"
	"fmt"
	"os"
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

	case "report":
		answer := str("answer")
		if answer == "" {
			answer = "(empty answer)"
		}
		fmt.Fprintf(os.Stdout, "\n  ✓ analysis result: %s\n", answer)
		return answer, stSuccess

	case "finish":
		ok, out := runGate(".")
		if ok {
			msg := "gate green"
			if out != "" {
				msg += "\n" + out
			}
			if advisory := runLintAdvisory("."); advisory != "" {
				msg += "\n" + advisory
			}
			return msg, stSuccess
		}
		*gateFails++
		return "gate FAILED (not done). Fix and call finish again:\n" + trim(out, 1200), stContinue

	case "run_gate":
		ok, out := runGate(".")
		if ok {
			msg := "gate green"
			if out != "" {
				msg += "\n" + out
			}
			if advisory := runLintAdvisory("."); advisory != "" {
				msg += "\n" + advisory
			}
			return msg, stContinue
		}
		return "gate red:\n" + trim(out, 1000), stContinue

	case "note":
		return appendNote(".", str("category"), str("text")), stContinue

	case "friction":
		r := FrictionReport{
			Task:                trim(cfg.Spec, 200),
			MissingCommand:      str("missing_command"),
			SuggestedSyntax:     str("suggested_syntax"),
			WorkaroundSteps:     splitLines(str("workaround_steps")),
			EstimatedStepsSaved: intArg(a, "estimated_steps_saved"),
		}
		if r.MissingCommand == "" {
			return "ERROR: friction needs missing_command (the gorefactor command that would have made this one step)", stContinue
		}
		logFriction(".", r)
		emitFrictionReport(cfg.Out, r)
		return "friction recorded; continue and call finish when done", stContinue

	case "list_symbols":
		return senseListSymbols(str("file")), stContinue

	case "read_excerpt":
		return senseReadExcerpt(str("file"), a), stContinue

	case "analyze_file_size":
		return senseFileSize(str("file")), stContinue

	case "find_references":
		return senseFindRefs(str("symbol")), stContinue

	case "inspect_file":
		return senseInspect(str("file")), stContinue

	case "skeleton":
		return senseSkeleton(str("file")), stContinue

	case "review_changes":
		return senseReview(str("ref")), stContinue

	case "lint_path":
		return senseLint(str("path")), stContinue

	case "extract_method":
		intStr := func(k string) string {
			if v, ok := a[k].(float64); ok {
				return fmt.Sprintf("%d", int(v))
			}
			s, _ := a[k].(string)
			return strings.TrimSpace(s)
		}
		return applyExtractMethod(str("file"), intStr("start_line"), intStr("end_line"), str("new_function_name")), stContinue

	case "split_file":
		return applySplitFile(str("file")), stContinue

	case "wrap_errors":
		return applyWrapErrors(str("file"), str("function")), stContinue

	case "set_doc":
		return applySetDoc(str("file"), str("declaration"), str("doc")), stContinue

	case "change_signature":
		return applyChangeSignature(a), stContinue

	case "rename_declaration", "replace_code", "insert_code",
		"create_file", "move_function", "move_method", "delete_declaration", "remove_code_block":
		return applyOp(call.Function.Name, a, cfg), stContinue

	case "insert_switch_case":
		return applyInsertSwitchCase(str("file"), str("symbol"), str("case_expr"), str("body")), stContinue
	case "insert_map_entry":
		return applyInsertMapEntry(str("file"), str("target"), str("element")), stContinue
	case "replace_in_literal":
		return applyReplaceInLiteral(str("file"), str("old"), str("new")), stContinue
	case "read_file":
		return senseReadFile(str("file")), stContinue
	case "replace_body":
		return applyReplaceBody(str("file"), str("symbol"), str("body")), stContinue
	case "add_field":
		return applyAddField(str("file"), str("struct"), str("field"), boolArg(a, "update_literals")), stContinue
	case "change_receiver":
		return applyChangeReceiver(str("file"), str("type_method"), str("mode")), stContinue
	case "extract_interface":
		return applyExtractInterface(str("file"), str("type"), str("interface_name")), stContinue
	case "inline":
		return applyInline(str("file"), str("function")), stContinue
	case "replace_text":
		return applyReplaceText(str("file"), str("symbol"), str("old"), str("new")), stContinue
	case "add_test":
		return applyAddTest(str("file"), str("symbol")), stContinue
	default:
		return "unknown tool: " + call.Function.Name, stContinue
	}
}
