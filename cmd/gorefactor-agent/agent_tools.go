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

// runLintAdvisory runs gorefactor lint and returns a brief advisory summary.
// It never fails — findings are informational and do not block the gate.
func runLintAdvisory(dir string) string {
	out, err := runIn(dir, gorefactorBin(), "lint", ".", "--json")
	if err != nil || strings.TrimSpace(out) == "" {
		return ""
	}
	var result struct {
		Issues []struct {
			File     string `json:"file"`
			Rule     string `json:"rule"`
			Severity string `json:"severity"`
			Message  string `json:"message"`
		} `json:"issues"`
		Summary struct {
			Total int `json:"total"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.Summary.Total == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  lint: %d issue(s) remaining (advisory)\n", result.Summary.Total)
	for i, iss := range result.Issues {
		if i >= 5 {
			fmt.Fprintf(&b, "  … and %d more\n", result.Summary.Total-5)
			break
		}
		fmt.Fprintf(&b, "  [%s] %s: %s\n", iss.Severity, iss.Rule, trim(iss.Message, 80))
	}
	return b.String()
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

// --- catalog & prompt ------------------------------------------------

func agenticSystemPrompt(dir string) string {
	files := fileList(dir)
	return `You are a mechanical Go refactoring agent. Change code ONLY via
the provided tools. Every mutation is a deterministic AST-correct
gorefactor op; go build+test is the only correctness judge.

GO SOURCE FILES:
` + files + `

SENSE TOOLS (read-only):
- list_symbols / read_excerpt / analyze_file_size / find_references: basic file queries
- inspect_file: one-page summary with lint hints and extraction candidates
- skeleton: file structure with bodies elided — good for orientation
- lint_path: run gorefactor lint; findings include autofixCmd hints
- review_changes: quality regression report vs a git ref

MUTATION TOOLS:
- extract_method, rename_declaration, replace_code, insert_code, create_file
- move_method, move_function, delete_declaration, remove_code_block
- split_file: auto-split an oversized file into sibling files
- wrap_errors <file> <function>: wrap bare 'return err' with fmt.Errorf
- set_doc <file> <declaration> <doc>: add/replace a godoc comment

RULES:
- Use ONLY paths from the file list above. Never guess paths.
- To rename/delete/move a symbol when the spec does NOT name its file,
  call find_references <symbol> FIRST. Never guess which file it's in.
- rename_declaration / delete_declaration need the symbol's identifier
  in function OR method OR type (not just new_name). Omitting it fails.
- Analysis-only tasks (find callers/uses, "where is X"): gather with
  sense tools, then call report with the answer. Do NOT call finish —
  no code changed, the gate is irrelevant.
- For extract_method: call list_symbols then read_excerpt to get exact
  line numbers. Do not guess line numbers.
- If the spec names the file and function, go straight to mutation.
- Call finish when done. If the gate fails, fix and call finish again.
- If the task needs logic you cannot express as a tool call, punt.`
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

// compile-time: ensure openAIProvider satisfies toolChatter.
var _ toolChatter = (*openAIProvider)(nil)
