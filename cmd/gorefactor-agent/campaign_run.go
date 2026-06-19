package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

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
