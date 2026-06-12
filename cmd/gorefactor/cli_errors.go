package main

import (
	"errors"
	"fmt"
	"strings"
)

// Semantic exit codes. Documented in printUsage; agents key retries off them.
const (
	exitOK          = 0 // success
	exitUsage       = 1 // usage / argument error
	exitNotFound    = 2 // target or pattern not found (semantic miss)
	exitParseError  = 3 // snippet or file does not parse
	exitGateFailure = 4 // build/test gate failed (doctor, --gate)
)

// cliError carries a semantic exit code and optional candidate symbols for
// "did you mean" style guidance and --json error payloads.
type cliError struct {
	code       int
	msg        string
	candidates []string
}

func (e *cliError) Error() string { return e.msg }

func usageErrorf(format string, a ...interface{}) error {
	return &cliError{code: exitUsage, msg: fmt.Sprintf(format, a...)}
}

func parseErrorf(format string, a ...interface{}) error {
	return &cliError{code: exitParseError, msg: fmt.Sprintf(format, a...)}
}

func gateErrorf(format string, a ...interface{}) error {
	return &cliError{code: exitGateFailure, msg: fmt.Sprintf(format, a...)}
}

func notFoundErrorf(format string, a ...interface{}) error {
	return &cliError{code: exitNotFound, msg: fmt.Sprintf(format, a...)}
}

// notFoundError builds an exit-2 error whose message lists the available
// candidates and, when one is close to name, a "did you mean" hint.
func notFoundError(summary, name string, candidates []string) error {
	var b strings.Builder
	b.WriteString(summary)
	if len(candidates) > 0 {
		b.WriteString("\navailable: ")
		b.WriteString(strings.Join(candidates, ", "))
		if hint := closestMatch(name, candidates); hint != "" {
			fmt.Fprintf(&b, "\ndid you mean %q?", hint)
		}
	}
	return &cliError{code: exitNotFound, msg: b.String(), candidates: candidates}
}

// exitCodeFor maps an error to its semantic exit code (default: usage error).
func exitCodeFor(err error) int {
	if err == nil {
		return exitOK
	}
	var ce *cliError
	if errors.As(err, &ce) {
		return ce.code
	}
	return exitUsage
}

// errCandidates extracts the candidate list from a cliError, if any.
func errCandidates(err error) []string {
	var ce *cliError
	if errors.As(err, &ce) {
		return ce.candidates
	}
	return nil
}

// closestMatch returns the candidate with the smallest Levenshtein distance
// to name, when that distance is small enough to be a plausible typo.
func closestMatch(name string, candidates []string) string {
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
