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

// ComputeScore sets r.Score from all findings (not just new ones — the score
// describes the tree, the gate describes the change). Only meaningful on
// full-tree runs; scoped callers should not request it.
func (r *Report) ComputeScore() {
	weighted := 0.0
	for _, f := range r.Findings {
		weighted += severityWeight[f.Severity]
	}
	score := 100.0 / (1.0 + weighted/scoreHalfLife)
	r.Score = &score
}
