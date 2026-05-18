package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
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

func senseFileSize(file string) string {
	if file == "" {
		return "ERROR: 'file' required"
	}
	iss, err := analyzer.AnalyzeFileSize(file, 300)
	if err != nil {
		return "ERROR analyzing file size: " + err.Error()
	}
	if iss == nil {
		return "ERROR: no result returned for " + file
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

func agenticSystemPrompt(dir string) string {
	map_ := codeMap(dir)
	return `You are a mechanical Go refactoring agent. You may ONLY change
code by calling the provided tools — there is no file editor and no
shell. Every mutation tool is a deterministic, AST-correct gorefactor
operation; the build/test gate is the only judge of correctness.

REPO LAYOUT (all non-test Go files):
` + map_ + `

FILE PATHS: Use only paths from the repo layout above. Do NOT guess
paths like "main.go" — Go projects keep source in subdirectories
(cmd/, pkg/, internal/, etc.). If a tool returns an error about a
missing file, you used the wrong path; consult the layout above and
retry with the correct one.

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
