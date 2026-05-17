// Command gorefactor-agent drives gorefactor with a cheap or local LLM.
//
// It is the inferential half of a harness: a small model proposes a
// constrained RefactoringPlan from an already-disambiguated spec, while
// gorefactor (deterministic) applies it and the Go toolchain gates it.
// The model never edits code and never sees line numbers.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
)

func main() {
	var (
		specFlag    = flag.String("spec", "", "refactoring spec text, or @path to read from a file")
		dir         = flag.String("dir", ".", "target Go module directory")
		providerK   = flag.String("provider", "openai", "model provider: openai (OpenAI-compatible) or anthropic")
		model       = flag.String("model", "gpt-4o-mini", "model name (cheap/local is the point)")
		apiBase     = flag.String("api-base", "", "provider base URL (default per provider; set for local/proxy)")
		maxIter     = flag.Int("max-iter", 3, "maximum attempts before giving up")
		dryRun      = flag.Bool("dry-run", false, "preview the plan and diff; never apply")
		allowDirty  = flag.Bool("allow-dirty", false, "skip the clean-git-worktree precondition")
		verbose     = flag.Bool("verbose", false, "echo the raw model response each iteration")
		printPrompt = flag.Bool("print-prompt", false, "print the assembled model prompt for the spec and exit (no model call)")
		showVersion = flag.Bool("version", false, "print version and exit")
		noSchema    = flag.Bool("no-schema", false, "disable decode-time JSON-schema enforcement (A/B)")
		agentic     = flag.Bool("agentic", false, "Arm D: agentic gorefactor-tools loop with punt (instead of single-shot plan)")
	)
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
	if spec == "" {
		fmt.Fprintln(os.Stderr, "Error: -spec is required (text or @file)")
		flag.Usage()
		os.Exit(2)
	}

	cfg := Config{
		Spec:       spec,
		Dir:        *dir,
		MaxIter:    *maxIter,
		DryRun:     *dryRun,
		AllowDirty: *allowDirty,
		Verbose:    *verbose,
		NoSchema:   *noSchema,
		Out:        os.Stdout,
	}

	if *printPrompt {
		fmt.Println("===== SYSTEM =====")
		fmt.Println(systemPrompt())
		fmt.Println("\n===== USER =====")
		fmt.Println(buildUserPrompt(spec, *dir, ""))
		return
	}

	provider := providerFromFlags(*providerK, *apiBase, *model)

	var runErr error
	if *agentic {
		tc, ok := provider.(toolChatter)
		if !ok {
			fmt.Fprintln(os.Stderr, "Error: -agentic requires a tool-calling provider (use -provider openai)")
			os.Exit(2)
		}
		runErr = RunAgenticDriver(context.Background(), tc, cfg)
	} else {
		runErr = RunDriver(context.Background(), provider, cfg)
	}
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
