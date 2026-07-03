package main

import (
	"encoding/json"
	"strings"
)

// agent_parse.go: pure extraction of the machine-readable blocks the agent
// emits (RUN_METRICS / FRICTION_REPORT / PUNT_REPORT) and classification of a
// run into one of the corpus's expected outcomes. No I/O, so it is unit-tested
// without invoking the LLM — the harness's own correctness gate.

// expectedOutcome is the coarse result the corpus predicts for a task and the
// runner observes from a real run.
type expectedOutcome string

const (
	outEfficient expectedOutcome = "efficient" // completed, no friction reported
	outFriction  expectedOutcome = "friction"  // completed, but a tool was missing/awkward
	outFail      expectedOutcome = "fail"      // punted (tool-gap or judgement)
	outError     expectedOutcome = "error"     // infrastructure error, not a signal
)

// runMetrics mirrors the JSON in the <<<RUN_METRICS … RUN_METRICS>>> block.
type runMetrics struct {
	Outcome     string `json:"outcome"` // fixed | punt | error
	Steps       int    `json:"steps"`
	LocalTokens int    `json:"local_tokens"`
}

// blockBody returns the text between "<<<MARKER" and "MARKER>>>", or "" if the
// markers are absent. Tolerant of the single-line RUN_METRICS form and the
// multi-line indented FRICTION/PUNT forms alike.
func blockBody(s, marker string) string {
	open := "<<<" + marker
	close := marker + ">>>"
	a := strings.Index(s, open)
	if a < 0 {
		return ""
	}
	rest := s[a+len(open):]
	b := strings.Index(rest, close)
	if b < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:b])
}

// parseRunMetrics extracts the RUN_METRICS record. ok is false when no block is
// present (e.g. the process died before emitting one).
func parseRunMetrics(stdout string) (rm runMetrics, ok bool) {
	body := blockBody(stdout, "RUN_METRICS")
	if body == "" {
		return runMetrics{}, false
	}
	if err := json.Unmarshal([]byte(body), &rm); err != nil {
		return runMetrics{}, false
	}
	return rm, true
}

// hasFriction reports whether the run emitted at least one friction report.
func hasFriction(stdout string) bool {
	return strings.Contains(stdout, "<<<FRICTION_REPORT")
}

// puntHasCapabilityGap reports whether a punt named a missing gorefactor
// command (a tool-gap punt) rather than a judgement punt.
func puntHasCapabilityGap(stdout string) bool {
	return strings.Contains(blockBody(stdout, "PUNT_REPORT"), "\"capability_gap\"")
}

// classifyOutcome maps a run's stdout to the coarse outcome the corpus
// compares against its expectation. RUN_METRICS.outcome is authoritative;
// friction refines a completion into efficient vs friction.
func classifyOutcome(stdout string) expectedOutcome {
	rm, ok := parseRunMetrics(stdout)
	if !ok {
		return outError
	}
	switch rm.Outcome {
	case "fixed":
		if hasFriction(stdout) {
			return outFriction
		}
		return outEfficient
	case "punt":
		return outFail
	default:
		return outError
	}
}
