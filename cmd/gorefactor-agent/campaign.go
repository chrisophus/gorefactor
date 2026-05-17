package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

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
	campaignFileSizeLimit = 300 // gorefactor's default oversize threshold
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

// RunCampaign is the autonomous second-tier worker: detect → delegate →
// commit-or-punt → repeat. Returns nil if it ran to a clean state or
// made progress; an error only on infrastructure failure.
func RunCampaign(ctx context.Context, tc toolChatter, cfg Config) error {
	if cfg.Out == nil {
		cfg.Out = os.Stdout
	}
	if !cfg.AllowDirty {
		if err := requireCleanWorktree(cfg.Dir); err != nil {
			return err
		}
	}
	prev, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(cfg.Dir); err != nil {
		return fmt.Errorf("chdir %s: %w", cfg.Dir, err)
	}
	defer os.Chdir(prev)

	t0 := time.Now()
	fixed, punted, handled, passesRun := 0, 0, 0, 0
	for pass := 1; pass <= campaignMaxPasses; pass++ {
		passesRun = pass
		findings := enumerateFindings(".")
		if len(findings) == 0 {
			fmt.Fprintf(cfg.Out, "\n✓ campaign: no deterministic findings; clean\n")
			break
		}
		fmt.Fprintf(cfg.Out, "\n══ pass %d: %d finding(s) ══\n", pass, len(findings))
		progressed := false
		for _, f := range findings {
			if handled >= campaignMaxFindings {
				fmt.Fprintf(cfg.Out, "  (finding ceiling %d reached)\n", campaignMaxFindings)
				break
			}
			handled++
			fmt.Fprintf(cfg.Out, "\n▶ %s: %s — %s\n", f.kind, f.path, f.detail)

			fcfg := cfg
			fcfg.Spec = f.spec
			fcfg.MaxIter = campaignStepBudget
			fcfg.AllowDirty = false // clean each time: committed wins / rolled-back punts

			err := RunAgenticDriver(ctx, tc, fcfg)
			switch {
			case err == nil:
				if cerr := commitFinding(f); cerr != nil {
					return fmt.Errorf("commit after fixing %s: %w", f.path, cerr)
				}
				fixed++
				progressed = true
				fmt.Fprintf(cfg.Out, "  ✓ fixed & committed: %s\n", f.path)
			case isPunt(err):
				punted++ // RunAgenticDriver already rolled back clean
				fmt.Fprintf(cfg.Out, "  ⮌ punted (left for the senior): %s\n", f.path)
			default:
				return fmt.Errorf("campaign infrastructure failure on %s: %w", f.path, err)
			}
		}
		if !progressed {
			fmt.Fprintf(cfg.Out, "\n(no progress this pass; stopping)\n")
			break
		}
	}

	ok, out := runGate(".")

	pt, ctk := 0, 0
	if ts, isTS := tc.(tokenStater); isTS {
		pt, ctk = ts.Tokens()
	}
	m := campaignMetrics{
		Findings: handled, Fixed: fixed, Punted: punted,
		Passes: passesRun, PromptTokens: pt, CompletionTokens: ctk,
		LocalTokens: pt + ctk, FrontierTokens: 0, WallMs: time.Since(t0).Milliseconds(),
		Note: "junior ran fully on the local model; every fixed finding is a frontier-token cost avoided; punts hand back warm with zero frontier spend",
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	fmt.Fprintf(cfg.Out, "\n══ campaign summary ══\n"+
		"  fixed:   %d (committed)\n  punted:  %d (warm-handed back)\n"+
		"  final gate: %s\n", fixed, punted, gateWord(ok))
	fmt.Fprintf(cfg.Out, "<<<CAMPAIGN_METRICS\n%s\nCAMPAIGN_METRICS>>>\n", string(b))
	if !ok {
		fmt.Fprintf(cfg.Out, "  gate detail: %s\n", trim(out, 800))
		return fmt.Errorf("campaign ended with a red gate")
	}
	return nil
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

// enumerateFindings runs gorefactor's deterministic sensors. v1:
// file-size oversize (fully deterministic detection; no LLM, no
// judgement). Future sensors (duplicates, extract candidates) append
// here behind the same delegate-or-punt contract.
func enumerateFindings(root string) []finding {
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
