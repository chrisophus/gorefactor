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

var refactorStopwords = map[string]bool{
	"rename": true, "move": true, "extract": true, "delete": true,
	"split": true, "inline": true, "add": true, "remove": true,
	"replace": true, "fix": true, "rewrite": true, "refactor": true,
	"wrap": true, "implement": true, "generate": true, "change": true,
	"update": true, "create": true, "insert": true, "use": true,
	"the": true, "a": true, "to": true, "into": true, "from": true,
	"in": true, "of": true, "for": true, "with": true, "and": true,
}

// isConfidentSymbol reports whether s has the shape of a real Go identifier rather than a plain
// English word that happened to be capitalized: a Receiver:Method reference, or a second uppercase
// letter past position 0 (CamelCase, e.g. ValidateOrder, OldHelper). Plain title-case English words
// ("Rename", "Bug") have neither.
func isConfidentSymbol(s string) bool {
	if strings.Contains(s, ":") {
		return true
	}
	upper := 0
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			upper++
		}
	}
	return upper >= 2
}

// primarySymbol returns the most likely real target identifier in the spec: the first match that
// looks like an actual Go symbol (confident), skipping known leading verbs; falling back to the
// first non-stopword match if nothing looks confident. Returns "" if nothing qualifies.
func primarySymbol(spec string) string {
	matches := reSpecSymbol.FindAllString(spec, -1)
	var fallback string
	for _, m := range matches {
		if refactorStopwords[strings.ToLower(m)] {
			continue // leading verb ("Rename", "Move", "Extract", ...), not a target
		}
		if isConfidentSymbol(m) {
			return m // CamelCase or Receiver:Method -- a real Go identifier shape
		}
		if fallback == "" {
			fallback = m // weak candidate: keep as a last resort
		}
	}
	return fallback
}

// blastRadiusScore runs `gorefactor blast-radius <sym> --json` and
// returns the composite score, or -1 when the symbol is empty,
// unresolved, or the binary is unavailable. Cheap and read-only; safe to
// call once per run for instrumentation.
func blastRadiusScore(dir, sym string) int {
	if strings.TrimSpace(sym) == "" {
		return -1
	}
	bin, ok := findGorefactorBin()
	if !ok {
		return -1
	}
	out, err := runIn(dir, bin, "blast-radius", sym, "--json")
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
