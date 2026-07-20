// Command gorefactor-agent drives gorefactor with a cheap or local LLM.
//
// It is the inferential half of a harness: a small model proposes
// deterministic gorefactor ops (via tool calling), while gorefactor applies
// them and the Go toolchain gates them. The model never edits code and
// never sees line numbers.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	var (
		specFlag    = flag.String("spec", "", "refactoring spec text, or @path to read from a file")
		dir         = flag.String("dir", ".", "target Go module directory")
		providerK   = flag.String("provider", "openai", "model provider: openai (OpenAI-compatible) or anthropic")
		model       = flag.String("model", "gpt-4o-mini", "model name (cheap/local is the point)")
		apiBase     = flag.String("api-base", "", "provider base URL (default per provider; set for local/proxy)")
		maxIter     = flag.Int("max-iter", 0, "max steps (0 = mode default: agentic 40)")
		allowDirty  = flag.Bool("allow-dirty", false, "skip the clean-git-worktree precondition")
		verbose     = flag.Bool("verbose", false, "echo raw model output / model prose")
		printPrompt = flag.Bool("print-prompt", false, "print the assembled prompt for the active mode and exit (no model call)")
		showVersion = flag.Bool("version", false, "print version and exit")
		campaign    = flag.Bool("campaign", false, "sensor-driven autonomous mode: gorefactor findings -> agentic fixes -> commit-or-punt (no -spec needed)")
		budget      = flag.Int("budget", 0, "token budget (prompt+completion) for the run; on exhaustion the agent stop-and-summarizes via a structured punt instead of wandering (0 = unlimited)")
	)
	flag.Parse()
	applyDoctorGateFlag()

	if *showVersion {
		fmt.Println(agentVersion)
		return
	}

	spec, err := resolveSpec(*specFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(2)
	}
	spec = resolveEmptySpec(spec, campaign)

	cfg := Config{
		Spec:       spec,
		Dir:        *dir,
		MaxIter:    *maxIter,
		AllowDirty: *allowDirty,
		Verbose:    *verbose,
		Budget:     *budget,
		Out:        os.Stdout,
	}

	if printAssembledPrompt(printPrompt, spec, dir) {
		return
	}

	// Triage guide: if the spec maps 1:1 to a deterministic gorefactor
	// op (rename, callers of X, ...), apply it and run the gate without
	// ever calling the LLM. RUN_METRICS is still emitted so the battery
	// sees a complete record. On no match, fall through to the agent.
	if matched, err := triage(cfg); matched {
		if err != nil {
			exitPuntAware(err)
		}
		return
	}

	providerDebug = *verbose

	provider := providerFromFlags(*providerK, *apiBase, *model)

	if runErr := runSelectedMode(campaign, provider, cfg); runErr != nil {
		// A punt is not a crash: the junior cleanly handed work back.
		// The structured report is already on stdout; exit 3 so a
		// delegating (senior) agent can branch on "punted" vs "failed".
		exitPuntAware(runErr)
	}
}

func printAssembledPrompt(printPrompt *bool, spec string, dir *string) (done bool) {
	if !*printPrompt {
		return false
	}
	fmt.Println("===== SYSTEM (agentic, default) =====")
	fmt.Println(agenticSystemPrompt(*dir))
	fmt.Println("\n===== TOOLS =====")
	for _, td := range toolCatalog() {
		fmt.Printf("- %s: %s\n", td.Function.Name, td.Function.Description)
	}
	fmt.Println("\n===== TASK =====")
	fmt.Println(strings.TrimSpace(spec))
	return true
}

func runSelectedMode(campaign *bool, provider toolChatter, cfg Config) error {
	if *campaign {
		return RunCampaign(context.Background(), provider, cfg)
	}
	return RunAgenticDriver(context.Background(), provider, cfg)
}

func resolveEmptySpec(spec string, campaign *bool) string {
	if spec == "" && !*campaign {
		fmt.Fprintln(os.Stderr, "Error: -spec is required (text or @file), or use -campaign")
		flag.Usage()
		os.Exit(2)
	}
	return spec
}

func exitPuntAware(err error) {
	var pe *puntError
	if errors.As(err, &pe) {
		fmt.Fprintln(os.Stderr, "punted:", pe.Error())
		os.Exit(3)
	}
	fmt.Fprintln(os.Stderr, "\nError:", err)
	os.Exit(1)
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
