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
	"strings"
)

func main() {
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
		singleShot  = flag.Bool("single-shot", false, "use the legacy single-shot constrained-plan path instead of the default agentic gorefactor-tools loop")
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
		if *singleShot {
			fmt.Println("===== SYSTEM (single-shot) =====")
			fmt.Println(systemPrompt())
			fmt.Println("\n===== USER =====")
			fmt.Println(buildUserPrompt(spec, *dir, ""))
		} else {
			fmt.Println("===== SYSTEM (agentic, default) =====")
			fmt.Println(agenticSystemPrompt())
			fmt.Println("\n===== TOOLS =====")
			for _, td := range toolCatalog() {
				fmt.Printf("- %s: %s\n", td.Function.Name, td.Function.Description)
			}
			fmt.Println("\n===== TASK =====")
			fmt.Println(strings.TrimSpace(spec))
		}
		return
	}

	provider := providerFromFlags(*providerK, *apiBase, *model)

	var runErr error
	if *singleShot {
		runErr = RunDriver(context.Background(), provider, cfg)
	} else {
		tc, ok := provider.(toolChatter)
		if !ok {
			fmt.Fprintln(os.Stderr,
				"Error: the default agentic mode needs a tool-calling provider (use -provider openai); or pass -single-shot")
			os.Exit(2)
		}
		runErr = RunAgenticDriver(context.Background(), tc, cfg)
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
