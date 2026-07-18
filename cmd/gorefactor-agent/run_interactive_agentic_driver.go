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
	autoRun := false
	reader := bufio.NewReader(os.Stdin)
	pause := func() bool {
		return interactivePause(ctx, tc, cfg, tools, reader, &autoRun, &messages)
	}

	for step := 1; step <= cfg.MaxIter; step++ {
		lastStep = step
		if done, perr := agenticPuntOnBudget(cfg, tc, trace, step, ""); done {
			return perr
		}
		fmt.Fprintf(cfg.Out, "\n── step %d/%d ──\n", step, cfg.MaxIter)
		asst, cerr := tc.ChatTools(ctx, assembleHistory(messages, historyKeep), tools)
		if cerr != nil {
			return fmt.Errorf("provider call failed: %w", cerr)
		}
		messages = append(messages, asst)

		// Always show model reasoning in interactive mode — this is the main
		// thing the user wants to see.
		if asst.Content != "" {
			fmt.Fprintf(cfg.Out, "\n  agent: %s\n", asst.Content)
		}

		if len(asst.ToolCalls) == 0 {
			if punted, perr := recordNoToolTurn(cfg, asst, step, &noTool, &trace); punted {
				return perr
			}
			// Give the user a chance to redirect before nudging the model.
			if pause() {
				return doPunt(cfg, "user_stop", "user stopped interactively", trace, step)
			}
			messages = appendToolNudge(messages)
			continue
		}
		noTool = 0

		if done, rerr := agenticToolRound(cfg, asst, step, &gateFails, &trace, &messages, pause); done {
			return rerr
		}
	}
	return doPunt(cfg, "autopunt:budget", "tool-call budget exhausted", trace, cfg.MaxIter)

}

func interactivePause(ctx context.Context, tc toolChatter, cfg Config, tools []toolDef, reader *bufio.Reader, autoRun *bool, messages *[]chatMessage) bool {
	if *autoRun {
		return false
	}
	stopped, goAuto, newMsgs := chatPause(ctx, tc, cfg, *messages, tools, reader)
	*messages = newMsgs
	*autoRun = goAuto
	return stopped
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
			resp, err := tc.ChatTools(ctx, assembleHistory(messages, historyKeep), tools)
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
