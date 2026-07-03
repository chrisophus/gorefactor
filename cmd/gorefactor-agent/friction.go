package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Slice 1a: friction capture -- the "succeeded, but a tool was missing or
// awkward" outcome. It is distinct from the failure corpus (which records
// only rejected ops and punts): a friction report is filed when the junior
// COMPLETED the task but had to chain several tools for what should have
// been one gorefactor command. It is the primary signal the senior uses to
// grow the tool catalog. Reports are appended to .gorefactor/friction.jsonl
// (gitignored, so it survives the git-clean rollback) and echoed inline as a
// <<<FRICTION_REPORT>>> block so a driving agent can extract it from the log.

const frictionRelPath = ".gorefactor/friction.jsonl"

// FrictionReport is one filed friction: the clumsy workaround actually used,
// the gorefactor command that would have collapsed it to a single step, and
// a rough estimate of the steps saved. Fields stay loose so a later
// classification pass can group by missing_command without a rigid schema.
type FrictionReport struct {
	TS                  string   `json:"ts"`
	Task                string   `json:"task"`
	MissingCommand      string   `json:"missing_command"`
	SuggestedSyntax     string   `json:"suggested_syntax"`
	WorkaroundSteps     []string `json:"workaround_steps,omitempty"`
	EstimatedStepsSaved int      `json:"estimated_steps_saved,omitempty"`
}

// logFriction appends one report to the friction corpus under dir.
// Best-effort, mirroring logFailure: any I/O error is swallowed because the
// corpus must never affect the run (it is a sensor, not a control surface).
func logFriction(dir string, r FrictionReport) {
	if r.TS == "" {
		r.TS = time.Now().UTC().Format(time.RFC3339)
	}
	path := filepath.Join(dir, frictionRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
}

// emitFrictionReport writes the machine-readable block a senior agent greps
// out of the run log, parallel to the PUNT_REPORT block in doPunt.
func emitFrictionReport(out io.Writer, r FrictionReport) {
	b, _ := json.MarshalIndent(r, "", "  ")
	fmt.Fprintf(out, "<<<FRICTION_REPORT\n%s\nFRICTION_REPORT>>>\n", string(b))
}

// splitLines splits a multi-line tool argument into trimmed, non-empty lines
// -- used to turn the model's free-form "workaround_steps" text into a slice.
func splitLines(s string) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// intArg reads an integer tool argument, tolerating the float64 that JSON
// unmarshalling into map[string]any produces for numbers.
func intArg(a map[string]any, k string) int {
	if v, ok := a[k].(float64); ok {
		return int(v)
	}
	return 0
}

// boolArg reads a boolean tool argument (false when absent or non-bool).
func boolArg(a map[string]any, k string) bool {
	b, _ := a[k].(bool)
	return b
}
