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
	maxToolCalls  = 40 // bounded autonomy: hard ceiling on agency (multi-file tasks need headroom)
	maxGateFails  = 4  // repeated red gate -> autopunt
	maxNoToolTurn = 3  // model keeps talking instead of acting -> autopunt
	// historyKeep is how many recent messages assembleHistory keeps in
	// full. Tuned up from the original 12 (a small-context local-model
	// assumption) so a frontier junior can hold a multi-file working set
	// in view and stop re-reading files it already saw.
	historyKeep = 24
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

// emitRunMetrics prints one machine-readable record per run: outcome +
// steps + local token usage + the primary target's blast-radius score.
// Frontier tokens are 0 by construction. The reliability battery
// aggregates these blocks; Phase 3 correlates blast_radius against
// tokens spent offline. A blastRadius of -1 means no target symbol was
// resolvable from the spec. tc is `any` (not toolChatter) so both the
// agentic drivers' toolChatter and the single-shot driver's Provider
// can be passed straight through to the tokenStater type-assertion.
func emitRunMetrics(out io.Writer, tc any, err error, steps, blastRadius int) {
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
		BlastRadius      int    `json:"blast_radius"`
	}{outcome, steps, pt, ct, pt + ct, 0, blastRadius}
	b, _ := json.Marshal(rec)
	fmt.Fprintf(out, "<<<RUN_METRICS %s RUN_METRICS>>>\n", string(b))
}

// tokensUsed returns cumulative prompt+completion tokens from any
// provider (agentic toolChatter or single-shot Provider), or 0 when it
// does not expose usage. Used by the Phase 2 budget check in every mode.
func tokensUsed(p any) int {
	if ts, ok := p.(tokenStater); ok {
		pt, ct := ts.Tokens()
		return pt + ct
	}
	return 0
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
	// CapabilityGap is set only when the junior punted because gorefactor
	// LACKS a needed command (not a judgement call). A pointer with
	// omitempty keeps a judgement punt's block byte-identical to before.
	CapabilityGap *CapabilityGap `json:"capability_gap,omitempty"`
}

// CapabilityGap is the structured "this tool is missing" signal a punt can
// carry, so the senior can distinguish a tool-gap punt (add the command)
// from a judgement punt (out of scope, leave it alone).
type CapabilityGap struct {
	MissingCommand  string `json:"missing_command"`
	SuggestedSyntax string `json:"suggested_syntax,omitempty"`
	WhatItWouldDo   string `json:"what_it_would_do,omitempty"`
}

// parseGap extracts a CapabilityGap from a punt tool call's arguments,
// returning nil when no missing_command was supplied (a judgement punt).
func parseGap(call toolCall) *CapabilityGap {
	var a struct {
		MissingCommand  string `json:"missing_command"`
		SuggestedSyntax string `json:"suggested_syntax"`
	}
	_ = json.Unmarshal([]byte(call.Function.Arguments), &a)
	if strings.TrimSpace(a.MissingCommand) == "" {
		return nil
	}
	return &CapabilityGap{
		MissingCommand:  strings.TrimSpace(a.MissingCommand),
		SuggestedSyntax: strings.TrimSpace(a.SuggestedSyntax),
	}
}

// puntError carries the report while satisfying error. Error() still
// contains "PUNT" so existing callers/tests keep working.
type puntError struct{ rep puntReport }

func (e *puntError) Error() string { return "PUNT: " + e.rep.Reason }

// Report exposes the structured hand-off (used by main.go / callers).
func (e *puntError) Report() puntReport { return e.rep }

// doPunt rolls back to the clean baseline, verifies it, emits the warm
// report, and returns a *puntError.
func doPunt(cfg Config, kind, reason string, trace []traceEntry, steps int, gaps ...*CapabilityGap) error {
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
	if len(gaps) > 0 && gaps[0] != nil {
		rep.CapabilityGap = gaps[0]
	}
	fmt.Fprintf(cfg.Out, "  ⮌ PUNT (%s): %s\n", kind, trim(reason, 400))
	b, _ := json.MarshalIndent(rep, "", "  ")
	fmt.Fprintf(cfg.Out, "<<<PUNT_REPORT\n%s\nPUNT_REPORT>>>\n", string(b))
	// Feed the two persistence surfaces: the failure corpus (Phase 6,
	// for classification) and cross-session notes (Phase 4, so the next
	// session does not re-attempt a task already known to be infeasible).
	logFailure(cfg.Dir, failureEntry{
		Kind:    failPunt,
		Reason:  trim(reason, 400),
		Spec:    trim(cfg.Spec, 200),
		Context: kind,
	})
	appendNote(cfg.Dir, "open_punt", fmt.Sprintf("%s: %s", kind, trim(reason, 160)))
	// A tool-gap punt additionally files a structured capability_gap row
	// and a tool_gap note so the senior (and future sessions) see the
	// missing command as a distinct, dedupable signal.
	if rep.CapabilityGap != nil {
		logFailure(cfg.Dir, failureEntry{
			Kind:    failCapabilityGap,
			Op:      rep.CapabilityGap.MissingCommand,
			Reason:  trim(reason, 400),
			Spec:    trim(cfg.Spec, 200),
			Context: rep.CapabilityGap.SuggestedSyntax,
		})
		appendNote(cfg.Dir, "tool_gap", fmt.Sprintf("missing %s: %s",
			rep.CapabilityGap.MissingCommand, trim(reason, 120)))
	}
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
	return prompt(files) + notesPromptSection(dir)
}

func prompt(files string) string {
	return `You are a capable Go engineer working through gorefactor's tools.
You change code ONLY via the provided tools — each mutation is a
deterministic AST-correct gorefactor op, so you never write broken code and
go build+test is the final judge. Work like a senior would: read each file
you will touch ONCE up front to build a mental model, then make your edits
directly. Do not re-read a file you have already seen this run.

GO SOURCE FILES:
` + files + `

SENSE TOOLS (read-only):
- read_file: read a WHOLE file in one call — use this first to orient on each
  target file. Prefer it over paging read_excerpt.
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
- change_signature <file> <symbol> <mode>: add/remove/rename a parameter
  AND update every call site in one op. Use this instead of deleting and
  re-inserting a function or hand-editing each caller.
- insert_switch_case <file> <symbol> <case_expr> [body]: add a case to the
  expression switch inside a function (wiring a new dispatch branch).
- insert_map_entry <file> <target> <element>: append an element to a
  composite literal — a package-level map/slice var, or a func-returned slice.
- replace_in_literal <file> <old> <new>: edit text inside one string literal
  anywhere in a file (e.g. a package-level prompt/const), AST-scoped.
- add_field <file> <struct> <field>: add a struct field (optional literal update).
- change_receiver <file> <type_method> <mode>: flip a receiver value<->pointer.
- extract_interface <file> <type> <interface_name>: interface from a type's methods.
- inline <file> <function>: inline a trivial function into callers and delete it.
- replace_text <file> <symbol> <old> <new>: literal text replace inside a body.
- add_test <file> <symbol>: scaffold a table-driven test for a function/method.
- replace_body <file> <symbol> <body>: replace the entire body of a named function or method.

NOTES:
- note <category> <text>: record a durable fact for future sessions.
  Categories: repo_fact, failed_strategy, flaky_test, open_punt. Use it
  when you learn something a later session would otherwise re-discover
  (a flaky package, a targeting strategy that fails here). Trust any
  PERSISTENT NOTES above before re-investigating.

FEEDBACK (help grow the toolset):
- friction <missing_command> <suggested_syntax>: if you COMPLETE the task
  but had to chain several tools for what should be one gorefactor command,
  call friction to record the gap. It does not block you — call finish
  after. This is how missing commands get added.
- When you punt because gorefactor LACKS a needed command (not a judgement
  call), pass missing_command/suggested_syntax to punt so the gap is logged.

RULES:
- Use ONLY paths from the file list above. Never guess paths.
- MULTI-FILE TASKS: call read_file on EACH target file ONCE, up front, to
  map every edit site; then make all edits. Do NOT re-read a file you have
  already seen this run — its contents are still in your history. Re-reading
  is the single biggest waste of your budget.
- TRUST SUCCESSFUL EDITS: every mutation is AST-verified and gofmt'd before
  it is written, so a success message means the edit is correct. Do NOT
  re-read a file just to confirm an edit landed.
- PREFER symbol/text-targeting tools (insert_switch_case, insert_map_entry,
  replace_in_literal, set_doc, change_signature) over line- or
  statement-exact tools (replace_code, remove_code_block): they target by
  name/text so you never need fresh line numbers after an edit shifts them.
  When adding a map/slice element, write just the element (a trailing comma
  is fine); when adding a switch case, give the case expression and body.
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
- If you completed the task but used a clumsy multi-step workaround for
  what should be one command, call friction(...) before finish.
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
