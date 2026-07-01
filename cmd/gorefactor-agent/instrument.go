package main

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Phase 3: blast-radius as a candidate cost predictor.
//
// Frontier models predict their own token cost at correlation 0.39 at
// best and underestimate systematically, so cost-based routing must not
// ask the model. gorefactor already computes a structural signal
// (blast-radius) that is a candidate predictor. This file instruments
// runs so the signal can be logged next to actual tokens spent and
// outcome; the correlation is then computed offline. Routing is NOT
// wired here -- per the plan it is gated on a measured correlation that
// beats 0.39, which requires the Phase 0 dataset. Until then this is
// pure instrumentation.

// reSpecSymbol pulls the most likely primary target symbol out of a
// spec: an exported-looking identifier, optionally in Receiver:Method
// form. Over-approximate on purpose -- the score is a ranking signal,
// not a decision, so a wrong guess only adds noise to the offline
// correlation, it never mis-routes a task.
var reSpecSymbol = regexp.MustCompile(`\b([A-Z][A-Za-z0-9_]*(?::[A-Za-z_][A-Za-z0-9_]*)?)\b`)

// primarySymbol returns the first exported-looking identifier in the
// spec, or "" if none is found.
func primarySymbol(spec string) string {
	m := reSpecSymbol.FindStringSubmatch(spec)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// blastRadiusScore runs `gorefactor blast-radius <sym> --json` and
// returns the composite score, or -1 when the symbol is empty,
// unresolved, or the binary is unavailable. Cheap and read-only; safe to
// call once per run for instrumentation.
func blastRadiusScore(dir, sym string) int {
	if strings.TrimSpace(sym) == "" {
		return -1
	}
	out, err := runIn(dir, gorefactorBin(), "blast-radius", sym, "--json")
	if err != nil || strings.TrimSpace(out) == "" {
		return -1
	}
	var br struct {
		Score int `json:"score"`
	}
	if json.Unmarshal([]byte(out), &br) != nil {
		return -1
	}
	return br.Score
}

// specBlastRadius is the convenience the drivers call: extract the
// primary symbol from the spec and score it. Returns -1 when there is
// nothing to score.
func specBlastRadius(dir, spec string) int {
	return blastRadiusScore(dir, primarySymbol(spec))
}
