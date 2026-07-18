package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Autofix outcomes are execution ground truth that static sensors cannot
// produce on their own: the verify gate actually built and ran the tree
// after each fix. The journal below persists those outcomes per finding
// fingerprint so later runs can act on them — skip re-attempting a fix the
// gate already rejected, and reclassify findings the outcome falsified
// (a dead-code deletion that breaks the gate proves the symbol reachable).
// The journal lives under the gitignored .gorefactor/ directory and is a
// passive sensor: appending or reading it never fails a fix run.

const (
	outcomeApplied  = "applied"
	outcomeReverted = "reverted"
	outcomeNoTarget = "no_target"
	outcomeVerified = "verified" // probe: fix applied cleanly and passed the gate, then was restored
)

// autofixOutcome is one journal record: what happened when the autofix for
// the fingerprinted finding was last attempted.
type autofixOutcome struct {
	Fingerprint string `json:"fingerprint"`
	Rule        string `json:"rule"`
	File        string `json:"file"`
	Outcome     string `json:"outcome"`
	Detail      string `json:"detail,omitempty"`
	Time        string `json:"time"`
}

func autofixOutcomesPath(root string) string {
	return filepath.Join(root, ".gorefactor", "autofix-outcomes.jsonl")
}

// recordOutcome builds a journal record for one issue.
func recordOutcome(iss lintIssue, outcome, detail string) autofixOutcome {
	return autofixOutcome{
		Fingerprint: issueFingerprint(iss),
		Rule:        iss.Rule,
		File:        iss.File,
		Outcome:     outcome,
		Detail:      detail,
		Time:        time.Now().UTC().Format(time.RFC3339),
	}
}

func truncateDetail(s string) string {
	const max = 400
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// appendAutofixOutcomes best-effort appends records to the journal.
func appendAutofixOutcomes(root string, recs []autofixOutcome) {
	if len(recs) == 0 {
		return
	}
	path := autofixOutcomesPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)

	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	for _, r := range recs {
		_ = enc.Encode(r)
	}
}

// loadAutofixOutcomes returns the most recent outcome per fingerprint, or an
// empty map when no journal exists.
func loadAutofixOutcomes(root string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(autofixOutcomesPath(root))
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var r autofixOutcome
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.Fingerprint != "" {
			out[r.Fingerprint] = r.Outcome
		}
	}
	return out
}

// annotateIssuesWithOutcomes rewrites findings using the journal. Notes go
// into the Note field — never the Message — so baseline and journal
// fingerprints stay stable across runs.
//
//   - dead-code whose deletion was gate-reverted demotes to info: the revert
//     is a proof the symbol is reachable by means the name-based scan cannot
//     see (reflection, build tags), i.e. the finding is falsified.
//   - other gate-reverted rules keep their severity but say what happened;
//     error-not-wrapped gets a specific hint, since a reverted wrap almost
//     always means a test depends on the exact error text.
//   - extraction rules whose fixer found nothing to extract stop promising
//     "consider extracting" silently: the honest next step is restructuring.
func annotateIssuesWithOutcomes(root string, issues []lintIssue) {
	if len(issues) == 0 {
		return
	}
	outcomes := loadAutofixOutcomes(root)
	if len(outcomes) == 0 {
		return
	}
	for i := range issues {
		if issues[i].Note != "" {
			continue // a live, fresher signal (e.g. the rule's own no-target check) wins
		}
		switch outcomes[issueFingerprint(issues[i])] {
		case outcomeReverted:
			switch issues[i].Rule {
			case "dead-code":
				issues[i].Severity = "info"
				issues[i].Note = "deletion previously failed the build/test gate — reachable by means the dead-code scan cannot see"
			case "error-not-wrapped":
				issues[i].Note = "mechanical wrap previously failed the gate — the error text may be contractual; compare with errors.Is or wrap deliberately"
			default:
				issues[i].Note = "autofix previously reverted by the build/test gate"
			}
		case outcomeNoTarget:
			switch issues[i].Rule {
			case "long-function", "complexity", "extract-candidate":
				issues[i].Note = "no mechanical extraction applies — needs restructuring, not extraction"
			}
		case outcomeVerified:
			issues[i].Note = "autofix verified safe by probe (gate passed)"
		}
	}
}

// skipKnownReverted partitions fixable issues into attemptable ones and
// ones whose fix the gate already rejected; retrying those would pay a
// build+test cycle to rediscover a recorded fact. Delete
// .gorefactor/autofix-outcomes.jsonl to retry after the code has changed
// enough that the fingerprint no longer matches anyway.
func skipKnownReverted(root string, fixable []lintIssue) (attempt []lintIssue, skipped int) {
	outcomes := loadAutofixOutcomes(root)
	if len(outcomes) == 0 {
		return fixable, 0
	}
	for _, iss := range fixable {
		if outcomes[issueFingerprint(iss)] == outcomeReverted {
			fmt.Fprintf(os.Stderr, "skip %s [%s]: fix previously reverted by gate (remove %s to retry)\n",
				iss.File, iss.Rule, autofixOutcomesPath(root))
			skipped++
			continue
		}
		attempt = append(attempt, iss)
	}
	return attempt, skipped
}
