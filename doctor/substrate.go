package doctor

import (
	"errors"
	"fmt"
)

// RunContext is what Diagnose hands each substrate.
type RunContext struct {
	// Root is the module directory to analyze.
	Root string
	// ScopeDirs restricts the run to these package dirs (relative to Root).
	// Empty means the whole tree. Only honored by scope-capable substrates.
	ScopeDirs []string
	// BaseRef is the git ref diff-based substrates (apidiff) compare against.
	BaseRef string
}

// SubstrateInfo describes a substrate's traits so Diagnose can tier and gate it.
type SubstrateInfo struct {
	Name string
	// Gating substrates can produce error-severity findings; in gate mode a
	// gating substrate that did not run is a dark sensor, not a pass.
	Gating bool
	// ScopeCapable substrates honor RunContext.ScopeDirs. Whole-program
	// substrates (deadcode, govulncheck) are not and run only on full passes.
	ScopeCapable bool
	// DiffBased substrates compute findings against BaseRef directly; their
	// findings are new by construction and they are exempt from baseline
	// builds and baseline marking.
	DiffBased bool
}

// Substrate is one detection engine merged into the Report.
type Substrate interface {
	Info() SubstrateInfo
	// Run returns findings, or an error wrapping ErrUnavailable when the
	// substrate could not run at all (missing binary, offline, no config) —
	// deliberately distinct from "ran and found problems" and from "crashed".
	Run(ctx RunContext) ([]Finding, error)
}

// ErrUnavailable marks a substrate that could not run at all. Advisory mode
// soft-skips it loudly; gate mode records it and campaigns fail fast at start.
var ErrUnavailable = errors.New("substrate unavailable")

// unavailablef wraps ErrUnavailable with a reason.
func unavailablef(format string, a ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, a...), ErrUnavailable)
}
