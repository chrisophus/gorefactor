package doctor

// Score layer (design plan step 7, optional and last): a presentation-only
// number derived from finding counts. Nothing gates on it (decision 4b) —
// counts and deltas carry the gating power; the score exists for dashboards
// and trend lines. The mapping is a smooth uncalibrated decay: 100 for a
// clean tree, halved at scoreHalfLife weighted findings. Calibration against
// real repos is future work; treat cross-repo comparisons as meaningless.

// scoreHalfLife is the weighted-finding count at which the score reaches 50.
const scoreHalfLife = 50.0

// severityWeight reflects the gate posture: errors dominate, infos barely
// register.
var severityWeight = map[Severity]float64{
	SeverityError:   3,
	SeverityWarning: 1,
	SeverityInfo:    0.25,
}

// scoreExemptRules are rules whose findings describe context or aspiration
// rather than an actionable defect in the current tree, so they must not drag
// the health score. high-blast-radius / low-gorefactor-adherence are ranking
// signals; the rest are advisory info-tier rules (coverage gaps, extraction
// suggestions, heuristics that admit they can't see enough to be sure). They
// still surface in `lint --info`; they just don't define "health".
var scoreExemptRules = map[string]bool{
	"high-blast-radius":        true,
	"low-gorefactor-adherence": true,
	"untested-function":        true,
	"untested-package":         true,
	"extract-candidate":        true,
	"linear-search-in-loop":    true,
	"naked-goroutine":          true,
	"pass-through-param":       true,
	"premature-abstraction":    true,
}

// ComputeScore sets r.Score from all findings (not just new ones — the score
// describes the tree, the gate describes the change). Only meaningful on
// full-tree runs; scoped callers should not request it.
func (r *Report) ComputeScore() {
	weighted := 0.0
	for _, f := range r.Findings {
		if scoreExemptRules[f.Rule] {
			continue
		}
		weighted += severityWeight[f.Severity]
	}

	score := 100.0 / (1.0 + weighted/scoreHalfLife)
	r.Score = &score
}
