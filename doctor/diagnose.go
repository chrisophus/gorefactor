package doctor

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
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
		report.ComputeScore(countScoredFunctions(opts.Root, cfg.WalkOptions()))
	}
	if !opts.NoJournal {
		if jerr := appendJournal(opts.Root, journalEntryFor(report)); jerr != nil {
			fmt.Fprintf(os.Stderr, "doctor: journal append failed: %v\n", jerr)
		}
	}
	return report, nil
}

func countScoredFunctions(root string, walk analyzer.WalkOptions) int {
	files, err := analyzer.WalkGoFiles(root, walk)
	if err != nil {
		return 0
	}
	fset := token.NewFileSet()
	n := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, perr := parser.ParseFile(fset, f, nil, 0)
		if perr != nil {
			continue
		}
		for _, d := range af.Decls {
			if _, ok := d.(*ast.FuncDecl); ok {
				n++
			}
		}
	}
	return n
}

// substrateResult is the output of one substrate execution.
type substrateResult struct {
	idx      int
	status   SubstrateStatus
	findings []Finding
}

// runSubstrates executes each substrate concurrently and records availability.
// It returns the merged findings and the set of diff-based substrate names
// (exempt from baseline marking). Results are merged in the original substrate
// order so output is deterministic.
func runSubstrates(opts Options, report *Report) ([]Finding, map[string]bool) {
	diffBased := map[string]bool{}
	type work struct {
		idx  int
		s    Substrate
		info SubstrateInfo
		rctx RunContext
		skip bool
	}

	// Build work items, recording diffBased and skipped substrates up front.
	items := make([]work, 0, len(opts.Substrates))
	for i, s := range opts.Substrates {
		info := s.Info()
		if info.DiffBased {
			diffBased[info.Name] = true
		}
		rctx := RunContext{Root: opts.Root, BaseRef: opts.BaseRef}
		skip := opts.Scoped && !info.ScopeCapable && !info.DiffBased
		if opts.Scoped && info.ScopeCapable {
			rctx.ScopeDirs = report.Scope
		}
		items = append(items, work{idx: i, s: s, info: info, rctx: rctx, skip: skip})
	}

	// Run non-skipped substrates in parallel.
	results := make([]substrateResult, len(items))
	ch := make(chan substrateResult, len(items))
	running := 0
	for _, w := range items {
		if w.skip {
			results[w.idx] = substrateResult{
				idx: w.idx,
				status: SubstrateStatus{
					Name:   w.info.Name,
					Gating: w.info.Gating,
					State:  SubstrateSkipped,
					Detail: "full-run-only substrate skipped in scoped run",
				},
			}
			continue
		}
		running++
		go func(w work) { ch <- runOneSubstrate(w.idx, w.s, w.info, w.rctx) }(w)
	}
	for i := 0; i < running; i++ {
		r := <-ch
		results[r.idx] = r
	}

	// Merge in original order for deterministic output.
	var findings []Finding
	for _, r := range results {
		report.Substrates = append(report.Substrates, r.status)
		findings = append(findings, r.findings...)
	}
	return findings, diffBased
}

// runOneSubstrate executes a single substrate and returns its result.
// Intended to be called in a goroutine by runSubstrates.
func runOneSubstrate(idx int, s Substrate, info SubstrateInfo, rctx RunContext) substrateResult {
	fs, err := s.Run(rctx)
	status := SubstrateStatus{Name: info.Name, Gating: info.Gating}
	switch {
	case errors.Is(err, ErrUnavailable):
		status.State = SubstrateSkipped
		status.Detail = err.Error()
		fs = nil
	case err != nil:
		status.State = SubstrateFailed
		status.Detail = err.Error()
		fs = nil
	default:
		status.State = SubstrateRan
		for i := range fs {
			fillDefaults(&fs[i], info.Name)
		}
	}
	return substrateResult{idx: idx, status: status, findings: fs}
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
