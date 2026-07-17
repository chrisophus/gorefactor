package doctor

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/chrisophus/gorefactor/config"
)

// Options configures one Diagnose run.
type Options struct {
	// Root is the module directory (default ".").
	Root string
	// BaseRef is the git ref findings are baselined against (default "HEAD").
	BaseRef string
	// Substrates to run. Callers compose these; cmd/gorefactor injects its
	// in-process structural linter alongside the library substrates.
	Substrates []Substrate
	// Scoped restricts scope-capable substrates to the packages touched vs
	// BaseRef plus depth-1 reverse deps. Non-scope-capable, non-diff-based
	// substrates are skipped in scoped runs (the full-run tier).
	Scoped bool
	// ConfigPath overrides .gorefactor.yaml discovery.
	ConfigPath string
	// NoJournal suppresses the doctor-history.jsonl append.
	NoJournal bool
	// Score computes the presentation-only score on full-tree runs
	// (decision 4b: nothing gates on it; ignored when Scoped).
	Score bool
}

// Diagnose runs the configured substrates, merges their findings into one
// Report, and marks each finding new or pre-existing against BaseRef. This is
// the programmatic API the CLI, the agent gate, and CI all consume.
func Diagnose(opts Options) (*Report, error) {
	if opts.Root == "" {
		opts.Root = "."
	}
	if opts.BaseRef == "" {
		opts.BaseRef = "HEAD"
	}
	cfg, err := config.Load(opts.ConfigPath, opts.Root)
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}

	report := &Report{
		SchemaVersion: SchemaVersion,
		BaseRef:       opts.BaseRef,
		NewCount:      map[Severity]int{},
	}
	if sha, serr := resolveSHA(opts.Root, opts.BaseRef); serr == nil {
		report.BaseSHA = sha
	}
	if opts.Scoped {
		scope, serr := ChangedScope(opts.Root, opts.BaseRef)
		if serr != nil {
			return nil, fmt.Errorf("compute scope: %w", serr)
		}
		report.Scope = scope
	}

	findings, diffBased := runSubstrates(opts, report)
	findings = normalizeFindings(findings, opts.Root, cfg.WalkOptions())
	findings = applyRuleTiers(findings, cfg)
	sortFindings(findings)
	markAgainstBaseline(findings, diffBased, opts, report)
	report.Findings = findings

	for _, f := range findings {
		if f.New {
			report.NewCount[f.Severity]++
		}
	}
	if opts.Score && !opts.Scoped {
		report.ComputeScore()
	}
	if !opts.NoJournal {
		if jerr := appendJournal(opts.Root, journalEntryFor(report)); jerr != nil {
			fmt.Fprintf(os.Stderr, "doctor: journal append failed: %v\n", jerr)
		}
	}
	return report, nil
}

// runSubstrates executes each substrate in its tier and records availability.
// It returns the merged findings and the set of diff-based substrate names
// (exempt from baseline marking).
func runSubstrates(opts Options, report *Report) ([]Finding, map[string]bool) {
	var findings []Finding
	diffBased := map[string]bool{}
	for _, s := range opts.Substrates {
		info := s.Info()
		if info.DiffBased {
			diffBased[info.Name] = true
		}
		status := SubstrateStatus{Name: info.Name, Gating: info.Gating}
		if opts.Scoped && !info.ScopeCapable && !info.DiffBased {
			status.State = SubstrateSkipped
			status.Detail = "full-run-only substrate skipped in scoped run"
			report.Substrates = append(report.Substrates, status)
			continue
		}
		rctx := RunContext{Root: opts.Root, BaseRef: opts.BaseRef}
		if opts.Scoped && info.ScopeCapable {
			rctx.ScopeDirs = report.Scope
		}
		fs, err := s.Run(rctx)
		switch {
		case errors.Is(err, ErrUnavailable):
			status.State = SubstrateSkipped
			status.Detail = err.Error()
		case err != nil:
			status.State = SubstrateFailed
			status.Detail = err.Error()
		default:
			status.State = SubstrateRan
			for i := range fs {
				fillDefaults(&fs[i], info.Name)
			}
			findings = append(findings, fs...)
		}
		report.Substrates = append(report.Substrates, status)
	}
	return findings, diffBased
}

// markAgainstBaseline loads (or builds) the base ref's fingerprint set and
// marks findings. Baseline failure (unknown ref, shallow clone) must not kill
// the run: everything is conservatively marked new and the failure is recorded
// as a pseudo-substrate status.
func markAgainstBaseline(findings []Finding, diffBased map[string]bool, opts Options, report *Report) {
	status := SubstrateStatus{Name: "baseline", State: SubstrateRan}
	base := BaselineSet{}
	if report.BaseSHA == "" {
		status.State = SubstrateFailed
		status.Detail = fmt.Sprintf("could not resolve base ref %q", opts.BaseRef)
	} else if set, err := LoadOrBuildBaseline(opts.Root, report.BaseSHA, opts.Substrates); err != nil {
		status.State = SubstrateFailed
		status.Detail = err.Error()
	} else {
		base = set
	}
	markNew(findings, base, diffBased)
	report.Substrates = append(report.Substrates, status)
	if status.State == SubstrateRan && !opts.Scoped {
		report.FixedCount = fixedCounts(findings, base, diffBased)
	}
}

// fixedCounts reports baseline findings absent from a full-tree run, per
// severity — meaningless on scoped runs, which see only part of the tree.
func fixedCounts(findings []Finding, base BaselineSet, diffBased map[string]bool) map[Severity]int {
	current := map[string]int{}
	for _, f := range findings {
		if !diffBased[f.Substrate] {
			current[f.Fingerprint]++
		}
	}
	fixed := map[Severity]int{}
	for fp, rec := range base {
		if gone := rec.Count - current[fp]; gone > 0 {
			fixed[rec.Severity] += gone
		}
	}
	if len(fixed) == 0 {
		return nil
	}
	return fixed
}

// fillDefaults completes a substrate finding: provenance, category-derived
// severity (plan decision 3b), and fingerprint.
func fillDefaults(f *Finding, substrate string) {
	if f.Substrate == "" {
		f.Substrate = substrate
	}
	if f.Category == "" {
		f.Category = CategoryLint
	}
	if f.Severity == "" {
		f.Severity = f.Category.DefaultSeverity()
	}
}

// applyRuleTiers applies .gorefactor.yaml rule tiers to findings by rule name:
// the config-demotion path for rules whose false-positive rate climbs
// (plan error-handling table). TierOff drops the finding entirely.
func applyRuleTiers(findings []Finding, cfg *config.File) []Finding {
	if cfg == nil || !cfg.HasRules() {
		return findings
	}
	out := findings[:0]
	for _, f := range findings {
		tier, ok := cfg.RuleTier(f.Rule, "")
		if ok {
			if tier == config.TierOff {
				continue
			}
			f.Severity = Severity(tier)
		}
		out = append(out, f)
	}
	return out
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Message < b.Message
	})
}
