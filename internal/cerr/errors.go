// Package cerr holds the semantic error classification shared by the gorefactor
// CLI and its importable refactoring engines. It carries a stable exit code and
// optional "did you mean" candidate list so that agents can key retries off the
// exit code and callers can render candidates uniformly.
package cerr

import (
	"errors"
	"fmt"
	"strings"
)

// Semantic exit codes. Documented in the CLI usage; agents key retries off them.
const (
	ExitOK          = 0 // success
	ExitUsage       = 1 // usage / argument error
	ExitNotFound    = 2 // target or pattern not found (semantic miss)
	ExitParseError  = 3 // snippet or file does not parse
	ExitGateFailure = 4 // build/test gate failed (doctor, --gate)
)

// CLIError carries a semantic exit code and optional candidate symbols for
// "did you mean" style guidance and --json error payloads. Its fields are
// exported so the CLI can both construct composite errors (e.g. txn wrapping)
// and inspect the code/message when deciding on fallbacks.
type CLIError struct {
	Code  int
	Msg   string
	Cands []string
}

func (e *CLIError) Error() string { return e.Msg }

// Usagef builds an exit-1 usage error.
func Usagef(format string, a ...interface{}) error {
	return &CLIError{Code: ExitUsage, Msg: fmt.Sprintf(format, a...)}
}

// Parsef builds an exit-3 parse error.
func Parsef(format string, a ...interface{}) error {
	return &CLIError{Code: ExitParseError, Msg: fmt.Sprintf(format, a...)}
}

// Gatef builds an exit-4 gate-failure error.
func Gatef(format string, a ...interface{}) error {
	return &CLIError{Code: ExitGateFailure, Msg: fmt.Sprintf(format, a...)}
}

// NotFoundf builds an exit-2 not-found error.
func NotFoundf(format string, a ...interface{}) error {
	return &CLIError{Code: ExitNotFound, Msg: fmt.Sprintf(format, a...)}
}

// NotFound builds an exit-2 error whose message lists the available candidates
// and, when one is close to name, a "did you mean" hint.
func NotFound(summary, name string, candidates []string) error {
	var b strings.Builder
	b.WriteString(summary)
	if len(candidates) > 0 {
		b.WriteString("\navailable: ")
		b.WriteString(strings.Join(candidates, ", "))
		if hint := ClosestMatch(name, candidates); hint != "" {
			fmt.Fprintf(&b, "\ndid you mean %q?", hint)
		}
	}
	return &CLIError{Code: ExitNotFound, Msg: b.String(), Cands: candidates}
}

// ExitCodeFor maps an error to its semantic exit code (default: usage error).
func ExitCodeFor(err error) int {
	if err == nil {
		return ExitOK
	}
	var ce *CLIError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ExitUsage
}

// Candidates extracts the candidate list from a CLIError, if any.
func Candidates(err error) []string {
	var ce *CLIError
	if errors.As(err, &ce) {
		return ce.Cands
	}
	return nil
}

// ClosestMatch returns the candidate with the smallest Levenshtein distance to
// name, when that distance is small enough to be a plausible typo.
func ClosestMatch(name string, candidates []string) string {
	best := ""
	bestDist := -1
	for _, c := range candidates {
		d := levenshtein(strings.ToLower(name), strings.ToLower(c))
		if bestDist < 0 || d < bestDist {
			best, bestDist = c, d
		}
	}
	if best == "" {
		return ""
	}
	limit := len(name) / 3
	if limit < 2 {
		limit = 2
	}
	if bestDist <= limit {
		return best
	}
	return ""
}

func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = minInt(prev[j]+1, minInt(curr[j-1]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
