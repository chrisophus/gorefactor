package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// RunAgenticDriver is Arm D's entry point. Mirror of RunDriver's safety
// envelope (clean-worktree precondition, chdir, git rollback) but the
// model drives via tool calls instead of one constrained plan.
func RunAgenticDriver(ctx context.Context, tc toolChatter, cfg Config) (err error) {
	if cfg.Out == nil {
		cfg.Out = os.Stdout
	}
	if cfg.MaxIter <= 0 {
		cfg.MaxIter = maxToolCalls
	}
	if !cfg.AllowDirty {
		if err := requireCleanWorktree(cfg.Dir); err != nil {
			return fmt.Errorf("require clean worktree: %w", err)
		}
	}
	prev, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	if err := os.Chdir(cfg.Dir); err != nil {
		return fmt.Errorf("chdir %s: %w", cfg.Dir, err)
	}
	defer os.Chdir(prev)

	lastStep := 0
	br := specBlastRadius(".", cfg.Spec)
	defer func() { emitRunMetrics(cfg.Out, tc, err, lastStep, br) }()

	messages := []chatMessage{
		{Role: "system", Content: agenticSystemPrompt(cfg.Dir)},
		{Role: "user", Content: "TASK:\n" + strings.TrimSpace(cfg.Spec)},
	}
	tools := toolCatalog()

	var trace []traceEntry
	gateFails, noTool := 0, 0
	for step := 1; step <= cfg.MaxIter; step++ {
		lastStep = step
		// Phase 2: stop-and-summarize before spending past the budget.
		// The journal/undo system makes this safe; any in-flight txn has
		// already committed or rolled back between steps.
		if done, perr := agenticPuntOnBudget(cfg, tc, trace, step,
			"; stopping with a clean summary rather than spending past the accuracy plateau"); done {
			return perr
		}
		fmt.Fprintf(cfg.Out, "\n── step %d/%d ──\n", step, cfg.MaxIter)
		asst, cerr := tc.ChatTools(ctx, assembleHistory(messages, historyKeep), tools)
		if cerr != nil {
			return fmt.Errorf("provider call failed: %w", cerr)
		}
		messages = append(messages, asst)
		if cfg.Verbose && asst.Content != "" {
			fmt.Fprintf(cfg.Out, "  thinking: %s\n", asst.Content)
		}

		if len(asst.ToolCalls) == 0 {
			if punted, perr := recordNoToolTurn(cfg, asst, step, &noTool, &trace); punted {
				return perr
			}
			messages = appendToolNudge(messages)
			continue
		}
		noTool = 0

		if done, rerr := agenticToolRound(cfg, asst, step, &gateFails, &trace, &messages, nil); done {
			return rerr
		}
	}
	return doPunt(cfg, "autopunt:budget", "tool-call budget exhausted", trace, cfg.MaxIter)

}

func agenticPuntOnBudget(cfg Config, tc toolChatter, trace []traceEntry, step int, detail string) (done bool, err error) {
	if cfg.Budget <= 0 {
		return false, nil
	}
	used := tokensUsed(tc)
	if used < cfg.Budget {
		return false, nil
	}
	logFailure(".", failureEntry{Kind: failBudgetHit,
		Reason:  fmt.Sprintf("token budget %d exhausted (used %d)", cfg.Budget, used),
		Spec:    trim(cfg.Spec, 200),
		Context: fmt.Sprintf("step %d", step)})
	return true, doPunt(cfg, "autopunt:budget_exhausted",
		fmt.Sprintf("token budget %d exhausted (used %d over %d step(s))%s",
			cfg.Budget, used, step-1, detail), trace, step)
}

func recordNoToolTurn(cfg Config, asst chatMessage, step int, noTool *int, trace *[]traceEntry) (punted bool, err error) {
	*noTool++
	*trace = addTrace(*trace, traceEntry{Step: step, Tool: "(no tool call)",
		Result: trim(asst.Content, 160)})
	if *noTool >= maxNoToolTurn {
		return true, doPunt(cfg, "autopunt:no_tool_calls",
			"model produced prose instead of tool calls repeatedly", *trace, step)
	}
	return false, nil
}

func appendToolNudge(messages []chatMessage) []chatMessage {
	return append(messages, chatMessage{Role: "user",
		Content: "Act via a tool. When the change is complete call finish. " +
			"If it cannot be done with these tools call punt(reason)."})
}

func agenticToolRound(cfg Config, asst chatMessage, step int, gateFails *int, trace *[]traceEntry, messages *[]chatMessage, pause func() bool) (done bool, err error) {
	for _, call := range asst.ToolCalls {
		content, status := dispatchTool(call, cfg, gateFails)
		recordRejectedOp(".", call.Function.Name, call.Function.Arguments, content, cfg.Spec)
		logToolCall(cfg.Out, cfg.Verbose, call.Function.Name, call.Function.Arguments, content)
		*trace = addTrace(*trace, traceEntry{Step: step, Tool: call.Function.Name,
			Args: trim(call.Function.Arguments, 200), Result: trim(content, 200)})
		*messages = append(*messages, chatMessage{
			Role: "tool", ToolCallID: call.ID, Content: content,
		})
		switch status {
		case stSuccess:
			fmt.Fprintf(cfg.Out, "  ✓ finished; gate green; changes kept\n")
			return true, nil
		case stPunt:
			return true, doPunt(cfg, "explicit", content, *trace, step, parseGap(call))
		}
		if pause != nil && pause() {
			return true, doPunt(cfg, "user_stop", "user stopped interactively", *trace, step)
		}
	}
	if *gateFails >= maxGateFails {
		return true, doPunt(cfg, "autopunt:gate_fails",
			fmt.Sprintf("gate failed %d times", *gateFails), *trace, step)
	}
	return false, nil
}
