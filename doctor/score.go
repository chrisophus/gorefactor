package doctor

import "sort"

// Score layer (design plan step 7, optional and last): a presentation-only
// number derived from finding counts. Nothing gates on it (decision 4b) —
// counts and deltas carry the gating power; the score exists for dashboards
// and trend lines. The mapping is a smooth uncalibrated decay: 100 for a
// clean tree, halved at scoreHalfLife weighted findings. Calibration against
// real repos is future work; treat cross-repo comparisons as meaningless.
//
// The weighting answers one question per rule: how reliably does fixing one
// of its findings produce a genuine maintainability, changeability,
// testability or performance improvement? Rules whose fixes are almost
// always real — consolidating a duplicate, wrapping an error, stopping a
// ticker, hoisting a regexp compile, deleting dead code — count at full
// severity weight. Size/shape proxies correlate with real cost but can be
// "fixed" by code motion alone (wrapping a whole body in one helper,
// bundling params into a struct that is unpacked on the next line), so they
// count at half weight: the pressure stays, the payoff for metric-shaped
// churn is halved. Conventions and context signals do not define health and
// count nothing.

// scoreHalfLife is the weighted-finding count at which the score reaches 50.
const scoreHalfLife = 50.0

// severityWeight reflects the gate posture: errors dominate, infos barely
// register. It is the default contribution for any rule the maps below do
// not classify (golangci/* findings, substrate findings, future rules).
var severityWeight = map[Severity]float64{
	SeverityError:   3,
	SeverityWarning: 1,
	SeverityInfo:    0.25,
}

// scoreProxyMultiplier discounts proxy-rule findings relative to their
// severity weight.
const scoreProxyMultiplier = 0.5

// scoreProxyRules are size/shape metrics: statistically correlated with
// maintenance cost, but their finding counts can be lowered by moving code
// around without making it better. They keep real weight — most fixes of
// these are genuine — but only half, so a refactor that clears proxies
// without touching defect-tier findings (duplication, error handling, dead
// code) shows up as the smaller win it is.
var scoreProxyRules = map[string]bool{
	"file-size":         true,
	"long-function":     true,
	"complexity":        true,
	"deep-nesting":      true,
	"god-object":        true,
	"large-class":       true,
	"fat-interface":     true,
	"excessive-params":  true,
	"excessive-returns": true,
	"data-clumps":       true,
	"type-switch":       true,
	"high-coupling":     true,
}

// scoreExemptRules are rules whose findings describe context, convention or
// aspiration rather than an actionable defect in the current tree, so they
// must not drag the health score. high-blast-radius /
// low-gorefactor-adherence are ranking signals; the funcorder family is
// declaration-order style, autofix-enforced by lint and cosmetic to
// maintainability; the rest are advisory info-tier heuristics that admit
// they can't see enough to be sure. They all still surface in `lint`; they
// just don't define "health".
var scoreExemptRules = map[string]bool{
	"high-blast-radius":        true,
	"low-gorefactor-adherence": true,
	"untested-function":        true,
	"extract-candidate":        true,
	"linear-search-in-loop":    true,
	"naked-goroutine":          true,
	"pass-through-param":       true,
	"premature-abstraction":    true,
	"funcorder-constructor":    true,
	"funcorder-struct-method":  true,
	"funcorder-function":       true,
}

// scoreWeightOverride pins a rule's contribution regardless of reported
// severity. untested-package reports at info severity so the gate never nags
// about it, but a package with no test file at all is an actionable
// testability defect whose fix — writing the package's first test — cannot
// be faked by code motion, so it scores like a warning. (untested-function
// stays exempt above: per-function coverage pressure belongs in review, not
// in a tree-health number.)
var scoreWeightOverride = map[string]float64{
	"untested-package": 1,
}

// ScoreClassifiedRules returns every rule name the score layer classifies
// away from its severity default (proxy, exempt or overridden). The CLI's
// rule-registry test cross-checks these against the real registry so a typo
// here cannot silently fall back to default weight.
func ScoreClassifiedRules() []string {
	out := make([]string, 0, len(scoreProxyRules)+len(scoreExemptRules)+len(scoreWeightOverride))
	for r := range scoreProxyRules {
		out = append(out, r)
	}
	for r := range scoreExemptRules {
		out = append(out, r)
	}
	for r := range scoreWeightOverride {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// scoreWeight returns the health-score contribution of one finding.
func scoreWeight(rule string, sev Severity) float64 {
	if w, ok := scoreWeightOverride[rule]; ok {
		return w
	}
	if scoreExemptRules[rule] {
		return 0
	}
	w := severityWeight[sev]
	if scoreProxyRules[rule] {
		w *= scoreProxyMultiplier
	}
	return w
}

// scoreProxyReferenceFuncs is the codebase size (in scored functions) at which
// the proxy tier is neither discounted nor amplified — the reference where this
// size-normalised score equals the older absolute-count score. 1000 functions
// is a mid-sized module; the proxy density is expressed per this many
// functions. It is the one calibration constant the normalisation introduces.
const scoreProxyReferenceFuncs = 1000.0

// ComputeScore sets r.Score from all findings (not just new ones — the score
// describes the tree, the gate describes the change). Only meaningful on
// full-tree runs; scoped callers should not request it.
//
// Defect-tier findings (duplication, error handling, dead code, gate integrity,
// lifecycle, …) are discrete flaws: N duplicate blocks is N flaws whether the
// repo is 5k or 50k LOC, so they count in absolute terms. Size/shape proxies
// (long-function, complexity, deep-nesting, data-clumps, …) fire per function
// above a threshold, so their expected count grows with the number of
// functions; summing them as an absolute count silently penalises a large
// codebase at constant quality. The proxy tier is therefore scored as a
// *density* — weighted proxy findings per scoreProxyReferenceFuncs functions —
// so the number measures the fraction of the codebase that is oversized, not
// its raw size. scoredFuncs below the reference is floored so small codebases
// are not divided into leniency, and a codebase of exactly the reference size
// scores identically to the pre-normalisation model.
func (r *Report) ComputeScore(scoredFuncs int) {
	defectWeighted := 0.0
	proxyWeighted := 0.0
	for _, f := range r.Findings {
		w := scoreWeight(f.Rule, f.Severity)
		if scoreProxyRules[f.Rule] {
			proxyWeighted += w
		} else {
			defectWeighted += w
		}
	}

	funcMultiple := float64(scoredFuncs) / scoreProxyReferenceFuncs
	if funcMultiple < 1.0 {
		funcMultiple = 1.0
	}
	weighted := defectWeighted + proxyWeighted/funcMultiple

	score := 100.0 / (1.0 + weighted/scoreHalfLife)
	r.Score = &score
}
