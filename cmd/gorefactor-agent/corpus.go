package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Phase 6: the Hashimoto failure corpus.
//
// Every op the deterministic layer rejects, every token-budget
// exhaustion, and every punt is appended to .gorefactor/failures.jsonl.
// The corpus is a passive sensor: it never gates a run. Its purpose is
// the mistake-cannot-recur loop -- recurring patterns are later
// classified into one of three engineered fixes (a new lint rule, a
// prompt amendment, or a new CLI capability) so the same mistake cannot
// happen twice.

const corpusRelPath = ".gorefactor/failures.jsonl"

// Failure-corpus record kinds.
const (
	failRejectedOp    = "rejected_op"    // the deterministic layer refused a proposed op
	failBudgetHit     = "budget_hit"     // a token budget was exhausted mid-task
	failPunt          = "punt"           // the agent handed the task back
	failCapabilityGap = "capability_gap" // punt blocked by a MISSING gorefactor command
)

// failureEntry is one line in the corpus. Fields are deliberately loose
// so a later classification pass (manual first, meta-agent later) can
// group by tool, op shape, or reason without a rigid schema.
type failureEntry struct {
	TS      string `json:"ts"`
	Kind    string `json:"kind"`
	Tool    string `json:"tool,omitempty"`
	Op      string `json:"op,omitempty"`
	Reason  string `json:"reason"`
	Spec    string `json:"spec,omitempty"`
	Context string `json:"context,omitempty"`
}

// logFailure appends one entry to the corpus under dir. Best-effort: any
// I/O error is swallowed because the corpus must never affect the run
// (it is a sensor, not a control mechanism).
func logFailure(dir string, e failureEntry) {
	if e.TS == "" {
		e.TS = time.Now().UTC().Format(time.RFC3339)
	}
	path := filepath.Join(dir, corpusRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
}

// isOpRejection reports whether a tool result string is a deterministic
// op rejection (ERROR/FAILED prefix) rather than a normal result. The
// apply* helpers and applyOp return these prefixes on refusal.
func isOpRejection(result string) bool {
	r := strings.TrimSpace(result)
	return strings.HasPrefix(r, "ERROR") || strings.HasPrefix(r, "FAILED")
}
func init() { mutationTools["replace_body"] = true }

// mutationTools is the set of tool names whose rejections are worth
// recording. Sense/control tools are excluded: a "not found" from a
// read-only query is not a harness defect signal.
var mutationTools = map[string]bool{
	"extract_method": true, "rename_declaration": true, "replace_code": true,
	"insert_code": true, "create_file": true, "move_function": true,
	"move_method": true, "delete_declaration": true, "remove_code_block": true,
	"split_file": true, "wrap_errors": true, "set_doc": true,
	"change_signature":   true,
	"insert_switch_case": true,
	"insert_map_entry":   true,
	"replace_in_literal": true,
	"add_field":          true,
	"change_receiver":    true,
	"extract_interface":  true,
	"inline":             true,
	"replace_text":       true,
	"add_test":           true,
}

// recordRejectedOp appends a rejected-op entry to the corpus when the
// tool is a mutation and its result is a rejection. Centralised at the
// loop's dispatch site so every driver (agentic, interactive, campaign)
// feeds the same corpus.
func recordRejectedOp(dir, tool, args, result, spec string) {
	if !mutationTools[tool] || !isOpRejection(result) {
		return
	}
	logFailure(dir, failureEntry{
		Kind:   failRejectedOp,
		Tool:   tool,
		Op:     trim(args, 300),
		Reason: trim(result, 400),
		Spec:   trim(spec, 200),
	})
}
