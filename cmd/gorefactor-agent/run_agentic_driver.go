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
	defer func() { emitRunMetrics(cfg.Out, tc, err, lastStep) }()

	messages := []chatMessage{
		{Role: "system", Content: agenticSystemPrompt(cfg.Dir)},
		{Role: "user", Content: "TASK:\n" + strings.TrimSpace(cfg.Spec)},
	}
	tools := toolCatalog()

	var trace []traceEntry
	gateFails, noTool := 0, 0
	for step := 1; step <= cfg.MaxIter; step++ {
		lastStep = step
		fmt.Fprintf(cfg.Out, "\n── step %d/%d ──\n", step, cfg.MaxIter)
		asst, err := tc.ChatTools(ctx, compactMessages(messages, 12), tools)
		if err != nil {
			return fmt.Errorf("provider call failed: %w", err)
		}
		messages = append(messages, asst)
		if cfg.Verbose && asst.Content != "" {
			fmt.Fprintf(cfg.Out, "  thinking: %s\n", asst.Content)
		}

		if len(asst.ToolCalls) == 0 {
			noTool++
			trace = addTrace(trace, traceEntry{Step: step, Tool: "(no tool call)",
				Result: trim(asst.Content, 160)})
			if noTool >= maxNoToolTurn {
				return doPunt(cfg, "autopunt:no_tool_calls",
					"model produced prose instead of tool calls repeatedly", trace, step)
			}
			messages = append(messages, chatMessage{Role: "user",
				Content: "Act via a tool. When the change is complete call finish. " +
					"If it cannot be done with these tools call punt(reason)."})
			continue
		}
		noTool = 0

		for _, call := range asst.ToolCalls {
			content, status := dispatchTool(call, cfg, &gateFails)
			logToolCall(cfg.Out, cfg.Verbose, call.Function.Name, call.Function.Arguments, content)
			trace = addTrace(trace, traceEntry{Step: step, Tool: call.Function.Name,
				Args: trim(call.Function.Arguments, 200), Result: trim(content, 200)})
			messages = append(messages, chatMessage{
				Role: "tool", ToolCallID: call.ID, Content: content,
			})
			switch status {
			case stSuccess:
				fmt.Fprintf(cfg.Out, "  ✓ finished; gate green; changes kept\n")
				return nil
			case stPunt:
				return doPunt(cfg, "explicit", content, trace, step)
			}
		}
		if gateFails >= maxGateFails {
			return doPunt(cfg, "autopunt:gate_fails",
				fmt.Sprintf("gate failed %d times", gateFails), trace, step)
		}
	}
	return doPunt(cfg, "autopunt:budget", "tool-call budget exhausted", trace, cfg.MaxIter)
}
