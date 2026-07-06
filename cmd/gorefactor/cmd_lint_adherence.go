package main

import (
	"fmt"
	"strings"
)

// adherenceFloor is the minimum number of modified files before the rule
// speaks — below it, a low ratio is noise (one raw edit out of two isn't a
// pattern). adherenceThreshold is the ratio under which the sensor warns.
const (
	adherenceFloor     = 3
	adherenceThreshold = 0.5
)

// lowAdherenceRule is the advisory sensor for the "prefer gorefactor over
// Write/Edit on .go files" rule. It is info-severity and never gates (there
// is no autofix — the corrective action is behavioral). It fires only when
// enough existing files were modified AND too few went through gorefactor,
// so it stays quiet on small or compliant diffs. Best-effort: any git or
// journal error yields no issue.
type lowAdherenceRule struct{}

func (lowAdherenceRule) Name() string { return "low-gorefactor-adherence" }

func (lowAdherenceRule) Run(ctx LintContext) []lintIssue {
	rep, err := computeAdherence("HEAD")
	if err != nil {
		return nil
	}
	ratio, ok := rep.ratio()
	if !ok || rep.ModifiedTotal < adherenceFloor || ratio >= adherenceThreshold {
		return nil
	}
	file := "."
	if len(rep.ModifiedRaw) > 0 {
		file = rep.ModifiedRaw[0]
	}
	return []lintIssue{{
		File:     file,
		Rule:     "low-gorefactor-adherence",
		Severity: "info",
		Message: fmt.Sprintf(
			"only %d/%d modified .go files went through gorefactor (%.0f%%); raw-edited: %s — prefer a gorefactor command (see CLAUDE.md) so edits are parse-checked and cheaper",
			rep.ModifiedAttributed, rep.ModifiedTotal, ratio*100, strings.Join(rep.ModifiedRaw, ", ")),
	}}
}
