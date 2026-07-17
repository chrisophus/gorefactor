// Package doctor implements the standalone codebase-health engine described in
// docs/doctor-design-plan.md: deterministic detection substrates merged into
// one Report, with fingerprint-based baseline marking so only new findings
// gate. Diagnose is the shared contract for the CLI, the agent loop, and CI.
package doctor

// SchemaVersion guards the JSON encoding of Report.
const SchemaVersion = 1

// Severity of a finding. Derived from Category (plan decision 3b), demotable
// per rule via .gorefactor.yaml rule tiers.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Category classifies a finding. Error-severity categories gate; the rest report.
type Category string

const (
	CategoryConc     Category = "conc"   // goroutine leaks, unguarded shared state, context misuse
	CategorySec      Category = "sec"    // gosec findings, reachable vulns
	CategoryAPI      Category = "api"    // undeclared exported-API changes
	CategoryTemporal Category = "tmprl"  // workflow-determinism violations
	CategoryPerf     Category = "perf"   // large copies, allocation patterns
	CategoryDead     Category = "dead"   // unused code, exports, dependencies
	CategoryStruct   Category = "struct" // structural-lint findings (size, duplication, smells, ordering)
	CategoryLint     Category = "lint"   // breadth-layer findings with no more specific category
)

// DefaultSeverity derives a finding's severity from its category.
func (c Category) DefaultSeverity() Severity {
	switch c {
	case CategoryConc, CategorySec, CategoryAPI, CategoryTemporal:
		return SeverityError
	default:
		return SeverityWarning
	}
}

// Finding is one diagnostic from one substrate, in the merged Report shape.
type Finding struct {
	File        string   `json:"file,omitempty"`
	Line        int      `json:"line,omitempty"`
	Rule        string   `json:"rule"`
	Substrate   string   `json:"substrate"`
	Category    Category `json:"category"`
	Severity    Severity `json:"severity"`
	Message     string   `json:"message"`
	New         bool     `json:"new"`
	Fingerprint string   `json:"fingerprint"`
	FixCmd      string   `json:"fixCmd,omitempty"`
	Context     string   `json:"context,omitempty"`
}

// SubstrateState records whether a substrate produced findings this run.
type SubstrateState string

const (
	SubstrateRan     SubstrateState = "ran"
	SubstrateSkipped SubstrateState = "skipped" // could not run (missing binary, offline, out of tier)
	SubstrateFailed  SubstrateState = "failed"  // started and errored
)

// SubstrateStatus is the per-substrate availability record. Gate mode treats a
// non-ran gating substrate as a dark sensor, never as a pass.
type SubstrateStatus struct {
	Name   string         `json:"name"`
	State  SubstrateState `json:"state"`
	Detail string         `json:"detail,omitempty"`
	Gating bool           `json:"gating"`
}

// Report is the merged result of one diagnose run.
type Report struct {
	SchemaVersion int               `json:"schemaVersion"`
	BaseRef       string            `json:"baseRef"`
	BaseSHA       string            `json:"baseSHA,omitempty"`
	Scope         []string          `json:"scope,omitempty"`
	Findings      []Finding         `json:"findings"`
	Substrates    []SubstrateStatus `json:"substrates"`
	NewCount      map[Severity]int  `json:"newCount"`
	// FixedCount is only populated on full-tree runs: a scoped run not seeing
	// a baseline fingerprint does not mean the issue was fixed.
	FixedCount map[Severity]int `json:"fixedCount,omitempty"`
	// Score is presentation-only and nil unless requested via Options.Score
	// on a full-tree run (plan decision 4b: nothing gates on it).
	Score *float64 `json:"score,omitempty"`
}

// NewErrors returns the new error-severity findings — the set that gates.
func (r *Report) NewErrors() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.New && f.Severity == SeverityError {
			out = append(out, f)
		}
	}
	return out
}

// DarkSubstrates returns gating substrates that did not run this pass.
func (r *Report) DarkSubstrates() []SubstrateStatus {
	var out []SubstrateStatus
	for _, s := range r.Substrates {
		if s.Gating && s.State != SubstrateRan {
			out = append(out, s)
		}
	}
	return out
}

// GateOK reports whether the gate passes: no new error-severity findings and,
// when requireSubstrates is set (gate mode), every gating substrate ran.
// Advisory callers pass requireSubstrates=false, matching doctor's soft-skip.
func (r *Report) GateOK(requireSubstrates bool) bool {
	if len(r.NewErrors()) > 0 {
		return false
	}
	if requireSubstrates && len(r.DarkSubstrates()) > 0 {
		return false
	}
	return true
}
