package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RunInteractiveAgenticDriver is the interactive version of the agentic loop.
// After each tool execution the user can chat freely with the agent, review
// the diff, stop, or continue. Any text that isn't a recognized command is
// sent to the model as a chat message and the agent responds before the next
// step begins.
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

		// Always show model reasoning in interactive mode — this is the main
		// thing the user wants to see.
		if asst.Content != "" {
			fmt.Fprintf(cfg.Out, "\n  agent: %s\n", asst.Content)
		}

		if len(asst.ToolCalls) == 0 {
			noTool++
			trace = addTrace(trace, traceEntry{Step: step, Tool: "(no tool call)",
				Result: trim(asst.Content, 160)})
			if noTool >= maxNoToolTurn {
				return doPunt(cfg, "autopunt:no_tool_calls",
					"model produced prose instead of tool calls repeatedly", trace, step)
			}
			if !autoRun {
				// Give the user a chance to redirect before nudging the model.
				stopped, goAuto, newMsgs := chatPause(ctx, tc, cfg, messages, tools, reader)
				messages = newMsgs
				autoRun = goAuto
				if stopped {
					return doPunt(cfg, "user_stop", "user stopped interactively", trace, step)
				}
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

			// Append tool result now so the message history is valid before
			// we open the chat pause (OpenAI requires tool results to follow
			// their tool calls before any user message).
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

			if !autoRun {
				stopped, goAuto, newMsgs := chatPause(ctx, tc, cfg, messages, tools, reader)
				messages = newMsgs
				autoRun = goAuto
				if stopped {
					return doPunt(cfg, "user_stop", "user stopped interactively", trace, step)
				}
			}
		}
		if gateFails >= maxGateFails {
			return doPunt(cfg, "autopunt:gate_fails",
				fmt.Sprintf("gate failed %d times", gateFails), trace, step)
		}
	}
	return doPunt(cfg, "autopunt:budget", "tool-call budget exhausted", trace, cfg.MaxIter)
}

// chatPause opens a conversational pause after a tool step. The user can type
// freely: any text goes to the model as a chat message and the agent responds.
// Recognized commands: enter/c = continue, r = diff, s = stop, a = auto.
// Returns (stopped, goAuto, updated messages).
const pausePrompt = "\n  ↩ continue · (a)uto · (r)eview diff · (s)top · or chat > "

func chatPause(ctx context.Context, tc toolChatter, cfg Config, messages []chatMessage, tools []toolDef, reader *bufio.Reader) (stopped, goAuto bool, newMsgs []chatMessage) {
	fmt.Fprint(cfg.Out, pausePrompt)
	for {
		input, err := reader.ReadString('\n')
		if err != nil {
			return false, false, messages // EOF → continue
		}
		input = strings.TrimSpace(input)

		switch input {
		case "", "c":
			return false, false, messages

		case "a":
			return false, true, messages

		case "s", "stop":
			return true, false, messages

		case "r", "diff":
			showGitDiff(cfg.Dir, cfg.Out)
			fmt.Fprint(cfg.Out, pausePrompt)

		default:
			// Free-form chat: send to model, show response, loop.
			messages = append(messages, chatMessage{Role: "user", Content: input})
			resp, err := tc.ChatTools(ctx, compactMessages(messages, 12), tools)
			if err != nil {
				fmt.Fprintf(cfg.Out, "  (error: %v)\n", err)
				fmt.Fprint(cfg.Out, pausePrompt)
				continue
			}
			messages = append(messages, resp)
			if resp.Content != "" {
				fmt.Fprintf(cfg.Out, "\n  agent: %s\n", resp.Content)
			}
			// If the model responded with tool calls, hand back to the main
			// loop to execute them rather than trying to dispatch here.
			if len(resp.ToolCalls) > 0 {
				return false, false, messages
			}
			fmt.Fprint(cfg.Out, pausePrompt)
		}
	}
}

// showGitDiff displays the current git diff to the user.
func showGitDiff(dir string, out interface{ Write([]byte) (int, error) }) {
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
