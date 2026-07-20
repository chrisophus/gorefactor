package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

// advertised-but-unwired generalizes the orphaned-config-path liveness idea
// from lesson 6 ("config that points at something must point at something that
// exists") to the other direction of the same drift: an artifact that
// *advertises* a capability the code does not wire. CLAUDE.md states the
// invariant directly — "if a feature is advertised (docs/examples) it must be
// wired; phantom surface area is a bug." The first member of this family
// checks example refactoring plans: every operation `type` in a plan JSON file
// must be a wired op type (orchestrator.KnownOperationTypes(), the same set the
// dispatcher and the doc-drift/plan-ops tests trust). An example plan that
// references an op type no executor dispatches would fail the moment a user
// ran it — a broken advertisement, exactly like an orphaned config path. It is
// detection-only: fixing it means editing prose/JSON or wiring an executor,
// neither a single safe mechanical transform.

type advertisedButUnwiredRule struct{}

func (advertisedButUnwiredRule) Name() string { return "advertised-but-unwired" }

func (r advertisedButUnwiredRule) Run(ctx LintContext) []lintIssue {
	root := ctx.Root
	if root == "" {
		root = "."
	}
	known := make(map[string]bool)
	for _, t := range orchestrator.KnownOperationTypes() {
		known[t] = true
	}

	var out []lintIssue
	for _, rel := range listTreePaths(root) {
		if !strings.HasSuffix(rel, ".json") || underTestdata(rel) {
			continue
		}
		out = append(out, unwiredOpTypesInPlan(root, rel, known)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Message < out[j].Message
	})
	return out
}

// planShape is the minimal subset of a RefactoringPlan the liveness check
// reads: the operation type list. Only the top-level operations[].type is
// consulted, never the nested target/condition/fallback "type" fields.
type planShape struct {
	Operations []struct {
		Type string `json:"type"`
	} `json:"operations"`
}

// unwiredOpTypesInPlan reports one finding per distinct unwired operation type
// in the JSON file at root/rel. Files that do not parse as a plan (or carry no
// operations) are silently skipped: this rule is a liveness check over genuine
// plan artifacts, not a JSON validator.
func unwiredOpTypesInPlan(root, rel string, known map[string]bool) []lintIssue {
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return nil
	}
	var plan planShape
	if err := json.Unmarshal(data, &plan); err != nil || len(plan.Operations) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []lintIssue
	for _, op := range plan.Operations {
		if op.Type == "" || known[op.Type] || seen[op.Type] {
			continue
		}
		seen[op.Type] = true
		out = append(out, lintIssue{
			File:     rel,
			Rule:     "advertised-but-unwired",
			Severity: "warning",
			Message: fmt.Sprintf("example plan advertises operation type %q, which no executor dispatches — wire a handler (orchestrator.RegisterExternalHandler) or remove it from the example (phantom surface area)",
				op.Type),
		})
	}
	return out
}
