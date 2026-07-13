// Command gorefactor-agent drives gorefactor with a cheap or local LLM.
//
// It is the inferential half of a harness: a small model proposes a
// constrained RefactoringPlan from an already-disambiguated spec, while
// gorefactor (deterministic) applies it and the Go toolchain gates it.
// The model never edits code and never sees line numbers.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	specFlag, dir, providerK, model, apiBase, maxIter, dryRun, allowDirty, verbose, printPrompt, showVersion, noSchema, singleShot, interactive, campaign, budget := extractBlockL20()
	flag.Parse()

	if *showVersion {
		fmt.Println(agentVersion)
		return
	}

	spec, err := resolveSpec(*specFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(2)
	}
	spec = extractBlockL50(spec, campaign, interactive)

	cfg := Config{
		Spec:       spec,
		Dir:        *dir,
		MaxIter:    *maxIter,
		DryRun:     *dryRun,
		AllowDirty: *allowDirty,
		Verbose:    *verbose,
		NoSchema:   *noSchema,
		Budget:     *budget,
		Out:        os.Stdout,
	}

	if extractBlockL64(printPrompt, singleShot, spec, dir) {
		return
	}

	// Triage guide: if the spec maps 1:1 to a deterministic gorefactor
	// op (rename, callers of X, ...), apply it and run the gate without
	// ever calling the LLM. RUN_METRICS is still emitted so the battery
	// sees a complete record. On no match, fall through to the agent.
	if matched, err := triage(cfg); matched {
		if err != nil {
			var pe *puntError
			if errors.As(err, &pe) {
				fmt.Fprintln(os.Stderr, "punted:", pe.Error())
				os.Exit(3)
			}
			fmt.Fprintln(os.Stderr, "\nError:", err)
			os.Exit(1)
		}
		return
	}

	providerDebug = *verbose

	provider := providerFromFlags(*providerK, *apiBase, *model)

	// Validate mode combinations
	if *interactive && (*singleShot || *campaign) {
		fmt.Fprintln(os.Stderr, "Error: -interactive is only for agentic mode (incompatible with -single-shot and -campaign)")
		os.Exit(2)
	}

	var runErr error
	runErr = extractBlockL111(campaign, provider, runErr, cfg, singleShot, interactive)
	if runErr != nil {
		// A punt is not a crash: the junior cleanly handed work back.
		// The structured report is already on stdout; exit 3 so a
		// delegating (senior) agent can branch on "punted" vs "failed".
		var pe *puntError
		if errors.As(runErr, &pe) {
			fmt.Fprintln(os.Stderr, "punted:", pe.Error())
			os.Exit(3)
		}
		fmt.Fprintln(os.Stderr, "\nError:", runErr)
		os.Exit(1)
	}
}

func extractBlockL20() (*string, *string, *string, *string, *string, *int, *bool, *bool, *bool, *bool, *bool, *bool, *bool, *bool, *bool, *int) {
	var (
		specFlag    = flag.String("spec", "", "refactoring spec text, or @path to read from a file")
		dir         = flag.String("dir", ".", "target Go module directory")
		providerK   = flag.String("provider", "openai", "model provider: openai (OpenAI-compatible) or anthropic")
		model       = flag.String("model", "gpt-4o-mini", "model name (cheap/local is the point)")
		apiBase     = flag.String("api-base", "", "provider base URL (default per provider; set for local/proxy)")
		maxIter     = flag.Int("max-iter", 0, "max steps/attempts (0 = mode default: agentic 24, single-shot 3)")
		dryRun      = flag.Bool("dry-run", false, "single-shot only: preview the plan and diff; never apply")
		allowDirty  = flag.Bool("allow-dirty", false, "skip the clean-git-worktree precondition")
		verbose     = flag.Bool("verbose", false, "echo raw model output / model prose")
		printPrompt = flag.Bool("print-prompt", false, "print the assembled prompt for the active mode and exit (no model call)")
		showVersion = flag.Bool("version", false, "print version and exit")
		noSchema    = flag.Bool("no-schema", false, "single-shot only: disable decode-time JSON-schema enforcement")
		singleShot  = flag.Bool("single-shot", false, "use the single-shot constrained-plan path (required for providers without tool-calling support; supports -dry-run preview)")
		interactive = flag.Bool("interactive", false, "agentic mode only: pause after each step for user feedback and guidance")
		campaign    = flag.Bool("campaign", false, "sensor-driven autonomous mode: gorefactor findings -> agentic fixes -> commit-or-punt (no -spec needed)")
		budget      = flag.Int("budget", 0, "token budget (prompt+completion) for the run; on exhaustion the agent stop-and-summarizes via a structured punt instead of wandering (0 = unlimited)")
	)
	return specFlag, dir, providerK, model, apiBase, maxIter, dryRun, allowDirty, verbose, printPrompt, showVersion, noSchema, singleShot, interactive, campaign, budget
}

func extractBlockL64(printPrompt *bool, singleShot *bool, spec string, dir *string) (done bool) {
	if *printPrompt {
		if *singleShot {
			fmt.Println("===== SYSTEM (single-shot) =====")
			fmt.Println(systemPrompt())
			fmt.Println("\n===== USER =====")
			fmt.Println(buildUserPrompt(spec, *dir, ""))
		} else {
			fmt.Println("===== SYSTEM (agentic, default) =====")
			fmt.Println(agenticSystemPrompt(*dir))
			fmt.Println("\n===== TOOLS =====")
			for _, td := range toolCatalog() {
				fmt.Printf("- %s: %s\n", td.Function.Name, td.Function.Description)
			}
			fmt.Println("\n===== TASK =====")
			fmt.Println(strings.TrimSpace(spec))
		}
		return true
	}
	return
}

func extractBlockL111(campaign *bool, provider Provider, runErr error, cfg Config, singleShot *bool, interactive *bool) error {
	switch {
	case *campaign:
		tc, ok := provider.(toolChatter)
		if !ok {
			fmt.Fprintln(os.Stderr, "Error: -campaign needs a tool-calling provider (use -provider openai)")
			os.Exit(2)
		}
		runErr = RunCampaign(context.Background(), tc, cfg)
	case *singleShot:
		runErr = RunDriver(context.Background(), provider, cfg)
	default:
		tc, ok := provider.(toolChatter)
		if !ok {
			fmt.Fprintln(os.Stderr,
				"Error: the default agentic mode needs a tool-calling provider (use -provider openai); or pass -single-shot")
			os.Exit(2)
		}
		if *interactive {
			runErr = RunInteractiveAgenticDriver(context.Background(), tc, cfg)
		} else {
			runErr = RunAgenticDriver(context.Background(), tc, cfg)
		}
	}
	return runErr
}

func extractBlockL50(spec string, campaign *bool, interactive *bool) string {
	if spec == "" && !*campaign {
		if *interactive {
			fmt.Print("What would you like to do? > ")
			reader := bufio.NewReader(os.Stdin)
			line, _ := reader.ReadString('\n')
			spec = strings.TrimSpace(line)
			if spec == "" {
				fmt.Fprintln(os.Stderr, "Error: no spec provided")
				os.Exit(2)
			}
		} else {
			fmt.Fprintln(os.Stderr, "Error: -spec is required (text or @file), or use -campaign")
			flag.Usage()
			os.Exit(2)
		}
	}
	return spec
}

// resolveSpec accepts inline text or @path.
func resolveSpec(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if s[0] == '@' {
		b, err := os.ReadFile(s[1:])
		if err != nil {
			return "", fmt.Errorf("reading spec file: %w", err)
		}
		return string(b), nil
	}
	return s, nil
}
