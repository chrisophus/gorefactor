package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/orchestrator"
	"github.com/chrisophus/gorefactor/parser"
)

// Arm D: gorefactor-as-tools agentic loop.
//
// qwen runs agentically but its ONLY effectors are gorefactor's
// deterministic ops + a build/test gate. It is structurally incapable
// of writing broken free-form code. When it cannot accomplish the task
// with these tools it calls punt() and we hand a clean baseline + a
// warm report back to whatever is driving it.

type toolStatus int

const (
	stContinue toolStatus = iota
	stSuccess             // finish() and the authoritative gate is green
	stPunt                // explicit punt() or an autopunt cap
)

const (
	maxToolCalls  = 24 // bounded autonomy: hard ceiling on agency
	maxGateFails  = 4  // repeated red gate -> autopunt
	maxNoToolTurn = 3  // model keeps talking instead of acting -> autopunt
)

// RunAgenticDriver is Arm D's entry point. Mirror of RunDriver's safety
// envelope (clean-worktree precondition, chdir, git rollback) but the
// model drives via tool calls instead of one constrained plan.
func RunAgenticDriver(ctx context.Context, tc toolChatter, cfg Config) error {
	if cfg.Out == nil {
		cfg.Out = os.Stdout
	}
	if cfg.MaxIter <= 0 {
		cfg.MaxIter = maxToolCalls
	}
	if !cfg.AllowDirty {
		if err := requireCleanWorktree(cfg.Dir); err != nil {
			return err
		}
	}
	prev, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(cfg.Dir); err != nil {
		return fmt.Errorf("chdir %s: %w", cfg.Dir, err)
	}
	defer os.Chdir(prev)

	messages := []chatMessage{
		{Role: "system", Content: agenticSystemPrompt()},
		{Role: "user", Content: "TASK:\n" + strings.TrimSpace(cfg.Spec)},
	}
	tools := toolCatalog()

	var trace []traceEntry
	gateFails, noTool := 0, 0
	for step := 1; step <= cfg.MaxIter; step++ {
		fmt.Fprintf(cfg.Out, "\n── step %d/%d ──\n", step, cfg.MaxIter)
		asst, err := tc.ChatTools(ctx, compactMessages(messages, 12), tools)
		if err != nil {
			return fmt.Errorf("provider call failed: %w", err)
		}
		messages = append(messages, asst)

		if len(asst.ToolCalls) == 0 {
			noTool++
			if cfg.Verbose && asst.Content != "" {
				fmt.Fprintf(cfg.Out, "  (model said: %s)\n", trim(asst.Content, 300))
			}
			trace = addTrace(trace, traceEntry{Step: step, Tool: "(no tool call)",
				Result: trim(asst.Content, 160)})
			if noTool >= maxNoToolTurn {
				return doPunt(cfg, "autopunt:no_tool_calls",
					"model produced prose instead of tool calls repeatedly", trace, step)
			}
			messages = append(messages, chatMessage{Role: "user",
				Content: "Act via a tool. When the change is complete call finish. " +
					"If it cannot be done with these tools call punt(reason)."})
			continue
		}
		noTool = 0

		for _, call := range asst.ToolCalls {
			content, status := dispatchTool(call, cfg, &gateFails)
			fmt.Fprintf(cfg.Out, "  → %s: %s\n", call.Function.Name, trim(content, 160))
			trace = addTrace(trace, traceEntry{Step: step, Tool: call.Function.Name,
				Args: trim(call.Function.Arguments, 200), Result: trim(content, 200)})
			messages = append(messages, chatMessage{
				Role: "tool", ToolCallID: call.ID, Content: content,
			})
			switch status {
			case stSuccess:
				fmt.Fprintf(cfg.Out, "  ✓ finished; gate green; changes kept\n")
				return nil
			case stPunt:
				return doPunt(cfg, "explicit", content, trace, step)
			}
		}
		if gateFails >= maxGateFails {
			return doPunt(cfg, "autopunt:gate_fails",
				fmt.Sprintf("gate failed %d times", gateFails), trace, step)
		}
	}
	return doPunt(cfg, "autopunt:budget", "tool-call budget exhausted", trace, cfg.MaxIter)
}

// traceEntry is one step the junior took, kept tight for the report.
type traceEntry struct {
	Step   int    `json:"step"`
	Tool   string `json:"tool"`
	Args   string `json:"args,omitempty"`
	Result string `json:"result,omitempty"`
}

// compactMessages bounds what we send the model so a long tool history
// never blows the (small, often 4096-token) context window. It keeps
// the system prompt + original task, elides the middle with a one-line
// marker, and keeps a recent window that starts on an assistant turn
// (so a tool message is never sent without its triggering tool call).
func compactMessages(msgs []chatMessage, keep int) []chatMessage {
	if len(msgs) <= keep+2 {
		return msgs
	}
	start := len(msgs) - keep
	for start > 2 && msgs[start].Role != "assistant" {
		start--
	}
	if start <= 2 {
		return msgs
	}
	out := make([]chatMessage, 0, 3+len(msgs)-start)
	out = append(out, msgs[0], msgs[1])
	out = append(out, chatMessage{Role: "user", Content: fmt.Sprintf(
		"(… %d earlier steps elided to fit context; continue from the latest results …)", start-2)})
	return append(out, msgs[start:]...)
}

func addTrace(t []traceEntry, e traceEntry) []traceEntry {
	t = append(t, e)
	if len(t) > 24 { // keep the tail; older steps rarely matter to the senior
		t = t[len(t)-24:]
	}
	return t
}

// puntReport is the warm hand-off the second-tier agent gives back to
// whatever delegated to it: the task, what it tried, why it stopped,
// and a guarantee the repo is clean. Emitted as JSON between markers
// so a senior agent can extract it from the run log; the process also
// exits with a distinct code (see main.go).
type puntReport struct {
	Status    string       `json:"status"` // always "punt"
	Kind      string       `json:"kind"`   // explicit | autopunt:*
	Task      string       `json:"task"`
	Reason    string       `json:"reason"`
	Steps     int          `json:"steps"`
	RepoClean bool         `json:"repo_clean"`
	Dir       string       `json:"dir"`
	Trace     []traceEntry `json:"trace"`
}

// puntError carries the report while satisfying error. Error() still
// contains "PUNT" so existing callers/tests keep working.
type puntError struct{ rep puntReport }

func (e *puntError) Error() string { return "PUNT: " + e.rep.Reason }

// Report exposes the structured hand-off (used by main.go / callers).
func (e *puntError) Report() puntReport { return e.rep }

// doPunt rolls back to the clean baseline, verifies it, emits the warm
// report, and returns a *puntError.
func doPunt(cfg Config, kind, reason string, trace []traceEntry, steps int) error {
	rollback(cfg.Dir, cfg.Out)
	rep := puntReport{
		Status:    "punt",
		Kind:      kind,
		Task:      strings.TrimSpace(cfg.Spec),
		Reason:    trim(reason, 800),
		Steps:     steps,
		RepoClean: requireCleanWorktree(cfg.Dir) == nil,
		Dir:       cfg.Dir,
		Trace:     trace,
	}
	fmt.Fprintf(cfg.Out, "  ⮌ PUNT (%s): %s\n", kind, trim(reason, 400))
	b, _ := json.MarshalIndent(rep, "", "  ")
	fmt.Fprintf(cfg.Out, "<<<PUNT_REPORT\n%s\nPUNT_REPORT>>>\n", string(b))
	return &puntError{rep: rep}
}

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

// --- sense tools (read-only, tight output per task #12) -------------

func senseListSymbols(file string) string {
	if file == "" {
		return "ERROR: 'file' required"
	}
	info, err := parser.ParseFile(file)
	if err != nil {
		return "ERROR: " + trim(err.Error(), 200)
	}
	var b strings.Builder
	for _, fn := range info.Functions {
		fmt.Fprintf(&b, "func %s\n", fn.Name)
	}
	for _, m := range info.Methods {
		fmt.Fprintf(&b, "method %s.%s\n", m.Receiver, m.Name)
	}
	return trim(b.String(), 1200)
}

func senseReadExcerpt(file string, a map[string]any) string {
	if file == "" {
		return "ERROR: 'file' required"
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "ERROR: " + trim(err.Error(), 200)
	}
	lines := strings.Split(string(data), "\n")
	num := func(k string, def int) int {
		if f, ok := a[k].(float64); ok {
			return int(f)
		}
		return def
	}
	start := num("start_line", 1)
	end := num("end_line", start+60)
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	// Tight window: small context budget (~4096 tok). A bigger view
	// should be taken as successive bounded reads, not one dump.
	if end-start > 80 {
		end = start + 80
	}
	if start > end {
		return "ERROR: start_line > end_line"
	}
	var b strings.Builder
	for i := start; i <= end; i++ {
		fmt.Fprintf(&b, "%d: %s\n", i, lines[i-1])
	}
	return trim(b.String(), 2800)
}

func senseFileSize(file string) string {
	if file == "" {
		return "ERROR: 'file' required"
	}
	iss, err := analyzer.AnalyzeFileSize(file, 300)
	if err != nil || iss == nil {
		return "ERROR analyzing file size"
	}
	b := &strings.Builder{}
	fmt.Fprintf(b, "lines=%d limit=%d oversized=%v\n", iss.LineCount, iss.MaxRecommended, iss.IsOversized)
	for i, h := range iss.ExtractionHints {
		if i >= 6 {
			break
		}
		fmt.Fprintf(b, "hint: %s (lines %d-%d, complexity %d, priority %d)\n",
			h.FunctionName, h.StartLine, h.EndLine, h.Complexity, h.Priority)
	}
	return trim(b.String(), 1000)
}

func senseFindRefs(symbol string) string {
	if symbol == "" {
		return "ERROR: 'symbol' required"
	}
	var b strings.Builder
	n := 0
	for _, f := range goFiles(".") {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, symbol) {
				fmt.Fprintf(&b, "%s:%d\n", f, i+1)
				if n++; n >= 40 {
					b.WriteString("…(more)\n")
					return b.String()
				}
			}
		}
	}
	if n == 0 {
		return "no references found"
	}
	return b.String()
}

// --- catalog & prompt ------------------------------------------------

func agenticSystemPrompt() string {
	return `You are a mechanical Go refactoring agent. You may ONLY change
code by calling the provided tools — there is no file editor and no
shell. Every mutation tool is a deterministic, AST-correct gorefactor
operation; the build/test gate is the only judge of correctness.

WORKFLOW:
1. Use sense tools (list_symbols, read_excerpt, analyze_file_size,
   find_references) to orient. Keep it minimal.
2. Apply mutation tools to make the change.
3. Call finish — it runs go build + go test. If it fails, fix and call
   finish again.
4. If the task CANNOT be accomplished with these tools (needs logic you
   cannot express as rename/move/insert/replace/create/delete), call
   punt with a clear reason. Punting is correct and expected when the
   tools do not fit — do NOT hack around it or fake completion.

Never claim done without calling finish. Be minimal and behaviour-safe.`
}

func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}
func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func tool(name, desc string, params map[string]any) toolDef {
	return toolDef{Type: "function", Function: toolDefFunction{Name: name, Description: desc, Parameters: params}}
}

func toolCatalog() []toolDef {
	return []toolDef{
		tool("list_symbols", "List functions/methods declared in a Go file.",
			obj(map[string]any{"file": strProp("relative path")}, "file")),
		tool("read_excerpt", "Read a bounded slice of a file (<=120 lines).",
			obj(map[string]any{"file": strProp("relative path"),
				"start_line": intProp("1-based start"), "end_line": intProp("end line")}, "file")),
		tool("analyze_file_size", "Report a file's size and extraction hints.",
			obj(map[string]any{"file": strProp("relative path")}, "file")),
		tool("find_references", "Find lines mentioning a symbol across the repo.",
			obj(map[string]any{"symbol": strProp("identifier")}, "symbol")),

		tool("rename_declaration", "Rename an unexported declaration package-wide.",
			obj(map[string]any{"file": strProp("file containing the decl"),
				"function": strProp("function name (or)"), "method": strProp("method name (or)"),
				"type": strProp("type name"), "new_name": strProp("new identifier")}, "file", "new_name")),
		tool("replace_code", "Replace a whole top-level statement inside a function.",
			obj(map[string]any{"file": strProp("file"), "function": strProp("enclosing function"),
				"code_pattern":     strProp("the EXACT complete statement to replace"),
				"replacement_code": strProp("equivalent replacement statement(s)")},
				"file", "function", "code_pattern", "replacement_code")),
		tool("insert_code", "Insert a new declaration into a file.",
			obj(map[string]any{"file": strProp("file"),
				"location_type":   strProp("at_end|after_function|before_function|at_beginning"),
				"anchor_function": strProp("anchor for *_function locations"),
				"code_snippet":    strProp("full declaration")}, "file", "location_type", "code_snippet")),
		tool("create_file", "Create a new file with full contents.",
			obj(map[string]any{"file": strProp("new path"),
				"code_snippet": strProp("complete file text incl. package clause")}, "file", "code_snippet")),
		tool("move_method", "Move a method to another file.",
			obj(map[string]any{"file": strProp("source file"), "method": strProp("method name"),
				"receiver_type": strProp("receiver type"), "new_file": strProp("destination file")},
				"file", "method", "receiver_type", "new_file")),
		tool("delete_declaration", "Delete a declaration.",
			obj(map[string]any{"file": strProp("file"), "function": strProp("function (or)"),
				"method": strProp("method (or)"), "type": strProp("type")}, "file")),
		tool("remove_code_block", "Remove a code block matching an exact pattern.",
			obj(map[string]any{"file": strProp("file"), "code_pattern": strProp("exact block")},
				"file", "code_pattern")),

		tool("run_gate", "Advisory: run go build + go test now.", obj(map[string]any{})),
		tool("finish", "Declare the task complete; runs the authoritative build+test gate.",
			obj(map[string]any{})),
		tool("punt", "Give up: the task cannot be done with these tools.",
			obj(map[string]any{"reason": strProp("why the tools do not fit")}, "reason")),
	}
}

// compile-time: ensure openAIProvider satisfies toolChatter.
var _ toolChatter = (*openAIProvider)(nil)
