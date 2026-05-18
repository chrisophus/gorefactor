package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// RunInteractiveAgenticDriver is the interactive version of the agentic loop.
// It pauses after each tool execution to let the user review and provide feedback.
func RunInteractiveAgenticDriver(ctx context.Context, tc toolChatter, cfg Config) (err error) {
	if cfg.Out == nil {
		cfg.Out = os.Stdout
	}
	if cfg.MaxIter <= 0 {
		cfg.MaxIter = maxToolCalls
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

	lastStep := 0
	defer func() { emitRunMetrics(cfg.Out, tc, err, lastStep) }()

	messages := []chatMessage{
		{Role: "system", Content: agenticSystemPrompt(cfg.Dir)},
		{Role: "user", Content: "TASK:\n" + strings.TrimSpace(cfg.Spec)},
	}
	tools := toolCatalog()

	var trace []traceEntry
	gateFails, noTool := 0, 0
	autoRun := false
	reader := bufio.NewReader(os.Stdin)

	for step := 1; step <= cfg.MaxIter; step++ {
		lastStep = step
		fmt.Fprintf(cfg.Out, "\n── step %d/%d ──\n", step, cfg.MaxIter)
		asst, err := tc.ChatTools(ctx, compactMessages(messages, 12), tools)
		if err != nil {
			return fmt.Errorf("provider call failed: %w", err)
		}
		messages = append(messages, asst)

		if len(asst.ToolCalls) == 0 {
			noTool++
			if cfg.Verbose && asst.Content != "" {
				fmt.Fprintf(cfg.Out, "  (model said: %s)\n", trim(asst.Content, 300))
			}
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
			fmt.Fprintf(cfg.Out, "  → %s: %s\n", call.Function.Name, trim(content, 160))
			trace = addTrace(trace, traceEntry{Step: step, Tool: call.Function.Name,
				Args: trim(call.Function.Arguments, 200), Result: trim(content, 200)})

			// Interactive pause point: before appending tool result to messages
			if !autoRun && status == stContinue {
				userFeedback := promptUser(cfg.Out, reader, step, cfg.MaxIter)
				switch {
				case userFeedback == "c" || userFeedback == "":
					// Continue normally
					messages = append(messages, chatMessage{
						Role: "tool", ToolCallID: call.ID, Content: content,
					})

				case strings.HasPrefix(userFeedback, "f "):
					// Provide feedback and continue
					feedback := strings.TrimPrefix(userFeedback, "f ")
					messages = append(messages, chatMessage{
						Role: "user",
						Content: fmt.Sprintf("User feedback: %s. Please refine your approach based on this guidance.",
							strings.TrimSpace(feedback)),
					})
					messages = append(messages, chatMessage{
						Role: "tool", ToolCallID: call.ID, Content: content,
					})

				case userFeedback == "r":
					// Show diff and re-prompt
					showGitDiff(cfg.Dir, cfg.Out)
					userFeedback = promptUser(cfg.Out, reader, step, cfg.MaxIter)
					if userFeedback == "s" {
						return doPunt(cfg, "user_stop", "user stopped via review", trace, step)
					}
					// Treat as continue after reviewing
					messages = append(messages, chatMessage{
						Role: "tool", ToolCallID: call.ID, Content: content,
					})

				case userFeedback == "s":
					// Stop and punt
					return doPunt(cfg, "user_stop", "user stopped interactively", trace, step)

				case userFeedback == "a":
					// Auto-continue: switch off pausing
					autoRun = true
					messages = append(messages, chatMessage{
						Role: "tool", ToolCallID: call.ID, Content: content,
					})

				case userFeedback == "?" || userFeedback == "help":
					// Show help and re-prompt
					showHelp(cfg.Out)
					userFeedback = promptUser(cfg.Out, reader, step, cfg.MaxIter)
					// Treat like "c" after help
					messages = append(messages, chatMessage{
						Role: "tool", ToolCallID: call.ID, Content: content,
					})

				default:
					// Unknown command, treat as continue
					messages = append(messages, chatMessage{
						Role: "tool", ToolCallID: call.ID, Content: content,
					})
				}
			} else {
				// Auto-run mode or special status: append normally
				messages = append(messages, chatMessage{
					Role: "tool", ToolCallID: call.ID, Content: content,
				})
			}

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

// promptUser displays the interactive prompt and reads user input.
func promptUser(out io.Writer, reader *bufio.Reader, step, maxIter int) string {
	fmt.Fprintf(out, "  Continue? [c/f/r/s/a/?] > ")
	input, err := reader.ReadString('\n')
	if err != nil {
		// EOF or other error: treat as "c" (continue)
		return "c"
	}
	return strings.TrimSpace(input)
}

// showGitDiff displays the current git diff to the user.
func showGitDiff(dir string, out io.Writer) {
	cmd := exec.Command("git", "-C", dir, "diff")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(out, "  (could not show diff: %v)\n", err)
		return
	}
	if len(output) == 0 {
		fmt.Fprintf(out, "  (no changes yet)\n")
		return
	}
	fmt.Fprintf(out, "  ┌─ git diff ──\n")
	fmt.Fprintf(out, "%s", indent(string(output)))
	fmt.Fprintf(out, "  └──\n")
}

// showHelp displays the interactive command help.
func showHelp(out io.Writer) {
	fmt.Fprintf(out, `
  Interactive Mode Commands:
    c           - Continue (accept this step and proceed)
    f <text>    - Provide feedback before continuing
    r           - Review changes so far (show git diff)
    s           - Stop (punt gracefully and rollback)
    a           - Auto-continue (stop pausing, finish autonomously)
    ? or help   - Show this help message
    <enter>     - Same as 'c' (continue)

`)
}
