package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

// Phase 2: sensor-driven campaign.
//
// gorefactor's deterministic sensors decide WHAT mechanical work
// exists (zero LLM). Each finding is delegated to the agentic Arm D
// loop, which does it or punts. A green fix is committed immediately
// so a later punt's `git reset --hard` only undoes the in-progress
// finding, never prior wins. The frontier model is never involved.

const (
	campaignFileSizeLimit = analyzer.DefaultMaxFileSize // gorefactor's default oversize threshold
	campaignStepBudget    = 20  // per-finding agentic budget
	campaignMaxPasses     = 5   // re-enumerate until clean / no progress
	campaignMaxFindings   = 12  // safety ceiling per run
)

type finding struct {
	kind   string
	path   string
	detail string
	spec   string
}

// campaignMetrics is the machine-readable run record. FrontierTokens
// is structurally 0: the junior never touches an expensive model, so
// LocalTokens (free) is the entire compute cost and every Fixed is a
// task the senior did not have to spend frontier tokens on.
type campaignMetrics struct {
	Findings         int    `json:"findings"`
	Fixed            int    `json:"fixed"`
	Punted           int    `json:"punted"`
	Passes           int    `json:"passes"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	LocalTokens      int    `json:"local_tokens"`
	FrontierTokens   int    `json:"frontier_tokens"`
	WallMs           int64  `json:"wall_ms"`
	Note             string `json:"note"`
}

// lintIssueJSON mirrors the JSON shape that `gorefactor lint --json` emits.
type lintIssueJSON struct {
	File       string `json:"file"`
	Rule       string `json:"rule"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	AutoFix    string `json:"autofix,omitempty"`
	AutoFixCmd string `json:"autofixCmd,omitempty"`
}

// enumerateFindings runs gorefactor's deterministic sensors and returns one
// finding per fixable lint issue. When the gorefactor binary is available it
// uses `lint --json` so every rule with an autofixCmd is autonomously fixable;
// otherwise it falls back to the direct file-size check.
func enumerateFindings(root string) []finding {
	if findings, ok := enumerateFindingsViaLint(root); ok {
		return findings
	}
	return enumerateFindingsFileSizeOnly(root)
}

// enumerateFindingsViaLint shells out to `gorefactor lint . --json`. Returns
// (nil, false) if the binary is unavailable or returns no parseable output.
func enumerateFindingsViaLint(root string) ([]finding, bool) {
	out, err := runIn(root, gorefactorBin(), "lint", ".", "--json")
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, false
	}

	var result struct {
		Issues []lintIssueJSON `json:"issues"`
	}
	if jerr := json.Unmarshal([]byte(out), &result); jerr != nil {
		return nil, false
	}

	// Group by (file, rule) so we create at most one finding per pair.
	// This keeps finding count bounded and gives the agent one clear task.
	type key struct{ file, rule string }
	seen := map[key]bool{}
	var findings []finding
	for _, iss := range result.Issues {
		if iss.AutoFixCmd == "" {
			continue
		}
		k := key{iss.File, iss.Rule}
		if seen[k] {
			continue
		}
		seen[k] = true
		findings = append(findings, finding{
			kind:   iss.Rule,
			path:   iss.File,
			detail: iss.Message,
			spec:   specFromLintIssue(iss),
		})
	}
	return findings, true
}

// specFromLintIssue builds an agent-friendly task description from one lint issue.
func specFromLintIssue(iss lintIssueJSON) string {
	parts := strings.Fields(iss.AutoFixCmd)
	if len(parts) < 2 {
		return fmt.Sprintf("Fix %s issue in %s: %s", iss.Rule, iss.File, iss.Message)
	}
	switch parts[1] {
	case "split":
		return fmt.Sprintf(
			"File %s is oversized. Use split_file to split it into smaller sibling files "+
				"in the same package (same directory, descriptive snake_case names). "+
				"Move whole top-level declarations only — never change logic. "+
				"If it cannot be done safely, punt.", iss.File)
	case "wrap-errors":
		fn := ""
		if len(parts) >= 4 {
			fn = parts[3]
		}
		return fmt.Sprintf(
			"Function %s in %s has bare 'return err' statements that should be wrapped. "+
				"Use wrap_errors to rewrite them with fmt.Errorf context. "+
				"If the function does not exist or the wrapping would break logic, punt.", fn, iss.File)
	case "set-doc":
		decl := ""
		if len(parts) >= 4 {
			decl = parts[3]
		}
		return fmt.Sprintf(
			"Declaration %s in %s is missing a godoc comment. "+
				"Use set_doc to add a brief one-sentence documentation comment "+
				"that describes what it does. Do not describe implementation details.", decl, iss.File)
	case "recommend":
		return fmt.Sprintf(
			"File %s has extraction candidates: %s. "+
				"Use inspect_file to find the most complex block, then use extract_method to extract it. "+
				"If no safe extraction is possible, punt.", iss.File, iss.Message)
	case "add-test":
		fn := ""
		if len(parts) >= 4 {
			fn = parts[3]
		}
		return fmt.Sprintf(
			"Function %s in %s has no test. "+
				"Use insert_code with at_end in the corresponding _test.go file to add a "+
				"table-driven test scaffold. If the file doesn't exist, create_file it first.", fn, iss.File)
	default:
		return fmt.Sprintf("Fix %s issue in %s: %s (hint: %s)",
			iss.Rule, iss.File, iss.Message, iss.AutoFixCmd)
	}
}

// enumerateFindingsFileSizeOnly is the fallback when the gorefactor binary is
// not available. It only detects file-size overages via the Go analyzer API.
func enumerateFindingsFileSizeOnly(root string) []finding {
	var out []finding
	for _, f := range goFiles(root) {
		iss, err := analyzer.AnalyzeFileSize(f, campaignFileSizeLimit)
		if err != nil || iss == nil || !iss.IsOversized {
			continue
		}
		out = append(out, finding{
			kind:   "file-size",
			path:   f,
			detail: fmt.Sprintf("%d lines (limit %d, over by %d)", iss.LineCount, iss.MaxRecommended, iss.OverageSize),
			spec: fmt.Sprintf(
				"The file %s is %d lines, over the %d-line limit. Split it: move whole "+
					"top-level declarations into one or more new sibling files in the same "+
					"package (same directory, descriptive snake_case names) so each file is "+
					"under the limit. Behaviour must be identical — only relocate declarations, "+
					"never change logic. If this cannot be done safely with the available "+
					"tools, punt.", f, iss.LineCount, campaignFileSizeLimit),
		})
	}
	return out
}

func commitFinding(f finding) error {
	if o, err := runIn(".", "git", "add", "-A"); err != nil {
		return fmt.Errorf("git add: %s", o)
	}
	msg := fmt.Sprintf("gorefactor-agent campaign: %s %s\n\n%s\n\nDelegated to the cheap second-tier agent (zero frontier tokens), gate-verified.\n\nCo-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>",
		f.kind, f.path, f.detail)
	if o, err := runIn(".", "git", "commit", "-q", "-m", msg); err != nil {
		return fmt.Errorf("git commit: %s", o)
	}
	return nil
}

func isPunt(err error) bool {
	var pe *puntError
	return errors.As(err, &pe)
}

func gateWord(ok bool) string {
	if ok {
		return "GREEN"
	}
	return "RED"
}
