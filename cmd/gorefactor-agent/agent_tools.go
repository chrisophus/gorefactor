package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// emitRunMetrics prints one machine-readable record per agentic run:
// outcome + steps + local token usage. Frontier tokens are 0 by
// construction. The reliability battery aggregates these blocks.
func emitRunMetrics(out io.Writer, tc toolChatter, err error, steps int) {
	outcome := "fixed"
	switch {
	case isPunt(err):
		outcome = "punt"
	case err != nil:
		outcome = "error"
	}
	pt, ct := 0, 0
	if ts, ok := tc.(tokenStater); ok {
		pt, ct = ts.Tokens()
	}
	rec := struct {
		Outcome          string `json:"outcome"`
		Steps            int    `json:"steps"`
		PromptTokens     int    `json:"prompt_tokens"`
		CompletionTokens int    `json:"completion_tokens"`
		LocalTokens      int    `json:"local_tokens"`
		FrontierTokens   int    `json:"frontier_tokens"`
	}{outcome, steps, pt, ct, pt + ct, 0}
	b, _ := json.Marshal(rec)
	fmt.Fprintf(out, "<<<RUN_METRICS %s RUN_METRICS>>>\n", string(b))
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

// gorefactorBin returns the path to the gorefactor binary, found as a sibling
// of the running agent binary so it works regardless of PATH.
func gorefactorBin() string {
	exe, err := os.Executable()
	if err == nil {
		sib := filepath.Join(filepath.Dir(exe), "gorefactor")
		if _, err := os.Stat(sib); err == nil {
			return sib
		}
	}
	return "gorefactor" // fall back to PATH
}

func logToolCall(out io.Writer, verbose bool, name, args, result string) {
	if verbose {
		fmt.Fprintf(out, "  → %s\n", name)
		if args != "" && args != "{}" {
			var pretty []byte
			if err := json.Unmarshal([]byte(args), new(any)); err == nil {
				pretty, _ = json.MarshalIndent(json.RawMessage(args), "    ", "  ")
			}
			if len(pretty) > 0 {
				fmt.Fprintf(out, "    args: %s\n", pretty)
			} else {
				fmt.Fprintf(out, "    args: %s\n", args)
			}
		}
		fmt.Fprintf(out, "    result: %s\n", trim(result, 2000))
	} else {
		fmt.Fprintf(out, "  → %s: %s\n", name, trim(result, 160))
	}
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

// --- catalog & prompt ------------------------------------------------

func agenticSystemPrompt(dir string) string {
	files := fileList(dir)
	return `You are a mechanical Go refactoring agent. Change code ONLY via
the provided tools. Every mutation is a deterministic AST-correct
gorefactor op; go build+test is the only correctness judge.

GO SOURCE FILES:
` + files + `

RULES:
- Use ONLY paths from the list above. Never guess paths like "main.go".
- If a file tool errors with "no such file", you used the wrong path.
- For extract_method: call list_symbols then read_excerpt to get exact
  line numbers before extracting. Do not guess line numbers.
- If the spec names the file and function, go straight to mutation —
  skip sense tools unless you need line numbers or content.
- Call finish when done. If the gate fails, fix and call finish again.
- If the task needs logic you cannot express as a tool call, punt with
  a clear reason. Punting is correct — do not fake completion.`
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
		// sense (read-only)
		tool("list_symbols", "List funcs/methods in a file.",
			obj(map[string]any{"file": strProp("path")}, "file")),
		tool("read_excerpt", "Read lines from a file (max 120).",
			obj(map[string]any{"file": strProp("path"),
				"start_line": intProp("1-based"), "end_line": intProp("inclusive")}, "file")),
		tool("analyze_file_size", "File line count and extraction hints.",
			obj(map[string]any{"file": strProp("path")}, "file")),
		tool("find_references", "Lines mentioning a symbol across the repo.",
			obj(map[string]any{"symbol": strProp("identifier")}, "symbol")),

		// mutation
		tool("extract_method", "Extract lines into a new function. Get line numbers via list_symbols+read_excerpt first.",
			obj(map[string]any{
				"file":              strProp("path"),
				"start_line":        intProp("first line (1-based)"),
				"end_line":          intProp("last line (inclusive)"),
				"new_function_name": strProp("new function name"),
			}, "file", "start_line", "end_line", "new_function_name")),
		tool("rename_declaration", "Rename an unexported declaration package-wide.",
			obj(map[string]any{"file": strProp("path"), "function": strProp("or"),
				"method": strProp("or"), "type": strProp("or"),
				"new_name": strProp("new identifier")}, "file", "new_name")),
		tool("replace_code", "Replace a complete statement inside a function.",
			obj(map[string]any{"file": strProp("path"), "function": strProp("enclosing func"),
				"code_pattern": strProp("exact statement to replace"), "replacement_code": strProp("replacement")},
				"file", "function", "code_pattern", "replacement_code")),
		tool("insert_code", "Insert a declaration into a file.",
			obj(map[string]any{"file": strProp("path"),
				"location_type":   strProp("at_end|after_function|before_function|at_beginning"),
				"anchor_function": strProp("anchor (for *_function)"),
				"code_snippet":    strProp("full declaration")}, "file", "location_type", "code_snippet")),
		tool("create_file", "Create a new Go file.",
			obj(map[string]any{"file": strProp("path"),
				"code_snippet": strProp("full file including package clause")}, "file", "code_snippet")),
		tool("move_method", "Move a method to another file.",
			obj(map[string]any{"file": strProp("source"), "method": strProp("method name"),
				"receiver_type": strProp("receiver type"), "new_file": strProp("destination")},
				"file", "method", "receiver_type", "new_file")),
		tool("delete_declaration", "Delete a func/method/type declaration.",
			obj(map[string]any{"file": strProp("path"), "function": strProp("or"),
				"method": strProp("or"), "type": strProp("or")}, "file")),
		tool("remove_code_block", "Remove a block matching an exact pattern.",
			obj(map[string]any{"file": strProp("path"), "code_pattern": strProp("exact block")},
				"file", "code_pattern")),

		// control
		tool("run_gate", "Run go build+test and report (advisory, non-terminal).", obj(map[string]any{})),
		tool("finish", "Mark task complete and run the authoritative gate.", obj(map[string]any{})),
		tool("punt", "Give up; task cannot be done with these tools.",
			obj(map[string]any{"reason": strProp("why")}, "reason")),
	}
}

// compile-time: ensure openAIProvider satisfies toolChatter.
var _ toolChatter = (*openAIProvider)(nil)
