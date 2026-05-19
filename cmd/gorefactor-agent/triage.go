package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// triage attempts a deterministic single-CLI-op shortcut for specs that
// map 1:1 to one gorefactor operation, skipping the LLM agent loop
// entirely. On a clean match it applies the op, runs the build/test
// gate (for mutations), emits RUN_METRICS, and returns matched=true.
// On no match — including ambiguous symbol resolution or a failed
// precondition — it returns matched=false so the caller falls through
// to the agentic loop. Triage is purely additive: it never regresses
// outcomes; on a positive match the worst it can do is exit 1 the same
// way an agent error would.
//
// This is a "guide" in the harness sense (Fowler's harness-engineering).
// The A/B in RELIABILITY-COMPARISON.md showed the battery's rename and
// analysis classes are 100% solved by one CLI command, so sending them
// through any LLM agent costs 10–100K tokens for what the CLI does for
// zero. The triage closes that misroute.
func triage(cfg Config) (matched bool, runErr error) {
	spec := strings.TrimSpace(cfg.Spec)
	if spec == "" {
		return false, nil
	}
	// Negative triage runs first: an explicit-judgement spec warm-punts at
	// 0 tokens before any positive pattern (e.g. "rewrite ... and rename ..."
	// stays as a punt rather than getting half-applied).
	if _, args, ok := matchInfeasible(spec); ok {
		return runAutoPunt(cfg, args)
	}
	// Safety guard: a spec carrying an explicit negative constraint ("rename X
	// to Y but not on receiver Z", "do not change W") cannot be safely handled
	// by regex-driven positive triage — the build+test gate validates syntax,
	// not the constraint. Discovered via RELIABILITY-HARD.md disambig: "Rename
	// Tokens to TokenUsage. Do NOT rename ..." matched reRename and the gate
	// stayed green even when both Tokens methods were renamed in lock-step.
	// Fall through to the agent so the model honors the constraint.
	if hasNegativeConstraint(spec) {
		return false, nil
	}
	for _, pat := range triagePatterns {
		if op, args, ok := pat.match(spec); ok {
			return runTriaged(cfg, pat.name, op, args)
		}
	}
	return false, nil
}

type triagePattern struct {
	name  string
	match func(spec string) (op string, args map[string]any, ok bool)
}

var triagePatterns = []triagePattern{
	{name: "rename", match: matchRename},
	{name: "callers", match: matchCallers},
}

// reRename anchors on the " to " connective: <OldIdent> immediately
// precedes " to " and <NewIdent> immediately follows. Anchoring on the
// connective rather than token order is what skips filler ("the
// unexported function") between "rename" and the actual symbol.
var reRename = regexp.MustCompile(`(?i)\brename\b[^.\n]*?\b([A-Za-z_][A-Za-z0-9_]*)\s+to\s+([A-Za-z_][A-Za-z0-9_]*)\b`)

func matchRename(spec string) (op string, args map[string]any, ok bool) {
	m := reRename.FindStringSubmatch(spec)
	if len(m) < 3 || m[1] == m[2] {
		return "", nil, false
	}
	return "rename_declaration", map[string]any{
		"function": m[1],
		"new_name": m[2],
	}, true
}

// reCallers: "callers of X" / "who calls X" with optional filler "the
// function|method". Captures the symbol that follows; no build/test
// gate because nothing changes — emits the file:line list as a report.
var reCallers = regexp.MustCompile(`(?i)\b(?:callers?\s+of|who\s+calls)\b\s+(?:the\s+)?(?:function\s+|method\s+)?([A-Za-z_][A-Za-z0-9_]*)\b`)

func matchCallers(spec string) (op string, args map[string]any, ok bool) {
	m := reCallers.FindStringSubmatch(spec)
	if len(m) < 2 {
		return "", nil, false
	}
	return "find_callers_report", map[string]any{"symbol": m[1]}, true
}

// runTriaged executes one matched op. Returns (matched, err):
//   - matched=false, err=nil: graceful fall-through (precondition failed,
//     symbol unresolvable, or chdir failed) — agent will pick it up.
//   - matched=true,  err=nil: op landed + gate green; RUN_METRICS emitted.
//   - matched=true,  err!=nil: apply/gate failed, worktree rolled back;
//     main.go exits 1, same surface as a normal agent error.
func runTriaged(cfg Config, name, op string, args map[string]any) (bool, error) {
	fmt.Fprintf(os.Stderr, "[triage] %s -> %s args=%v (no LLM call)\n", name, op, args)
	if !cfg.AllowDirty {
		if err := requireCleanWorktree(cfg.Dir); err != nil {
			return false, nil
		}
	}
	prev, _ := os.Getwd()
	if cfg.Dir != "" {
		if err := os.Chdir(cfg.Dir); err != nil {
			return false, nil
		}
		defer os.Chdir(prev)
	}

	if op == "find_callers_report" {
		sym, _ := args["symbol"].(string)
		out := senseFindRefs(sym)
		fmt.Fprintf(cfg.Out, "report: callers of %s\n%s\n", sym, out)
		emitRunMetrics(cfg.Out, nil, nil, 1)
		fmt.Fprintln(cfg.Out, "  ✓ triage finished; analysis-only (no gate)")
		return true, nil
	}

	// Mutation path: pre-resolve the symbol's file so rename_declaration
	// can target it. If the symbol isn't uniquely resolvable, fall
	// through — preserving the "never regress" property.
	if sym, _ := args["function"].(string); sym != "" {
		if f, corrected := resolveSymbolFile(sym, ""); corrected {
			args["file"] = f
		} else {
			fmt.Fprintf(os.Stderr,
				"[triage] symbol %q not uniquely resolved; falling back to agent\n", sym)
			return false, nil
		}
	}
	res := applyOp(op, args, cfg)
	if strings.HasPrefix(res, "ERROR:") || strings.HasPrefix(res, "FAILED:") {
		rollback(".", cfg.Out)
		emitRunMetrics(cfg.Out, nil, fmt.Errorf("triage %s failed", op), 1)
		return true, fmt.Errorf("triage %s: %s", op, trim(res, 200))
	}
	fmt.Fprintf(cfg.Out, "[triage] %s\n", res)
	ok, gateOut := runGate(".")
	if !ok {
		rollback(".", cfg.Out)
		emitRunMetrics(cfg.Out, nil, fmt.Errorf("gate red"), 1)
		return true, fmt.Errorf("triage gate red after %s:\n%s", op, trim(gateOut, 400))
	}
	fmt.Fprintln(cfg.Out, "  ✓ triage finished; gate green")
	emitRunMetrics(cfg.Out, nil, nil, 1)
	return true, nil
}

var reInfeasible = regexp.MustCompile(`(?i)` +
	`\b(?:rewrite|refactor|optimi[sz]e|reimplement|redesign)\b[^.\n]*?` +
	`\b(?:performance|linear[- ]time|big[- ]o|complexity|rolling[- ]hash|` +
	`algorithm|design|architecture|memory|speed|allocation|approach)\b` +
	`|\bfix\b[^.\n]*?\b(?:race|deadlock|concurrency|leak)\b`)

func matchInfeasible(spec string) (op string, args map[string]any, ok bool) {
	if reInfeasible.MatchString(spec) {
		return "autopunt_judgement", map[string]any{"spec": spec}, true
	}
	return "", nil, false
}

func runAutoPunt(cfg Config, args map[string]any) (bool, error) {
	fmt.Fprintln(os.Stderr,
		"[triage] infeasible -> autopunt:judgement (no LLM call)")
	reason := "spec explicitly requires semantic judgement " +
		"(algorithm rewrite / race fix / performance redesign); " +
		"senior should pick this up. See RELIABILITY-COMPARISON.md."
	err := doPunt(cfg, "autopunt:judgement", reason, nil, 1)
	emitRunMetrics(cfg.Out, nil, err, 1)
	return true, err
}

var reNegConstraint = regexp.MustCompile(`(?i)\b(?:do not|don't|but not|except|leave[^.\n]{0,60}(?:alone|untouched|unchanged)|without (?:modifying|changing|touching)|only (?:on|the|in))\b`)

func hasNegativeConstraint(spec string) bool { return reNegConstraint.MatchString(spec) }
