// benchmark measures token-context efficiency of gorefactor vs. direct file editing.
//
// For each scenario it computes:
//   - direct_chars:   bytes the LLM must read (source files) + write (full modified file)
//   - refactor_chars: bytes the LLM must produce to invoke the gorefactor command
//   - bytes it receives back as command output
//
// The ratio direct/refactor shows how many fewer tokens the gorefactor path
// exposes to the model.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	dir := flag.String("dir", "..", "root of the gorefactor module")
	verbose := flag.Bool("v", false, "print scenario detail")
	agentCorpus := flag.Bool("agent-corpus", false, "list the agent task corpus (Slice 2), no LLM calls")
	agentCorpusRun := flag.Bool("agent-corpus-run", false, "execute the agent task corpus against the junior (costs tokens)")
	only := flag.String("only", "", "corpus: run only the task with this id")
	difficulty := flag.String("difficulty", "", "corpus: filter by difficulty (easy|medium|hard)")
	mineFailures := flag.Bool("mine-failures", false, "mine .gorefactor/failures.jsonl into eval-task stubs (Slice 3b), no LLM calls")
	minCount := flag.Int("min-count", 2, "mine-failures: minimum cluster size to graduate a failure into a task stub")
	emitTasks := flag.Bool("emit-tasks", false, "mine-failures: write reviewable task stubs to .gorefactor/mined_tasks.go.txt")
	provider := flag.String("provider", "anthropic", "corpus: agent provider")
	model := flag.String("model", "claude-sonnet-4-6", "corpus: agent model")
	models := flag.String("models", "", "corpus: comma-separated model sweep (provider inferred per model; overrides -model)")
	modes := flag.String("modes", "", "corpus: comma-separated harness-mode sweep (agentic)")
	budget := flag.Int("budget", 500000, "corpus: per-task token budget")
	flag.Parse()

	root, err := filepath.Abs(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if *mineFailures {
		if _, err := runMineFailures(root, *minCount, *emitTasks, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "mine-failures:", err)
			os.Exit(1)
		}
		return
	}

	if *agentCorpus || *agentCorpusRun {
		runAgentCorpus(root, corpusOpts{
			run: *agentCorpusRun, only: *only, difficulty: *difficulty,
			provider: *provider, model: *model, models: *models, modes: *modes,
			budget: *budget, verbose: *verbose,
		})
		return
	}

	bin := filepath.Join(root, "gorefactor")
	if _, err := os.Stat(bin); err != nil {
		fmt.Fprintln(os.Stderr, "build gorefactor first: go build -o gorefactor ./cmd/gorefactor")
		os.Exit(1)
	}

	scenarios := buildScenarios(root)

	fmt.Printf("%-42s  %9s  %9s  %6s  %7s  %s\n",
		"scenario", "direct", "refactor", "ratio", "build", "category")
	fmt.Println(strings.Repeat("-", 97))

	var totalDirect, totalRefactor int
	for _, s := range scenarios {
		result := s.run(root, bin, *verbose)
		totalDirect += result.directChars
		totalRefactor += result.refactorChars

		ratio := 0.0
		if result.refactorChars > 0 {
			ratio = float64(result.directChars) / float64(result.refactorChars)
		}
		build := "n/a"
		if result.buildRan {
			build = "OK"
			if !result.buildPassed {
				build = "FAIL"
			}
		}
		fmt.Printf("%-42s  %9d  %9d  %5.0fx  %7s  %s\n",
			s.Name, result.directChars, result.refactorChars, ratio, build, s.Category)
	}

	totalRatio := 0.0
	if totalRefactor > 0 {
		totalRatio = float64(totalDirect) / float64(totalRefactor)
	}
	fmt.Println(strings.Repeat("-", 97))
	fmt.Printf("%-42s  %9d  %9d  %5.0fx\n", "TOTAL", totalDirect, totalRefactor, totalRatio)
	fmt.Printf("\ndirect_chars   = bytes LLM must read+write using only file I/O tools\n")
	fmt.Printf("refactor_chars = bytes to invoke gorefactor command + receive its output\n")
	fmt.Printf("ratio          = context-token savings when using gorefactor\n")
}

type result struct {
	directChars   int
	refactorChars int
	buildRan      bool
	buildPassed   bool
}

type scenario struct {
	Name                string
	Category            string
	ReadFiles           []string
	WriteEstimate       int
	GoRefactorArgs      []string
	ExpectedOutputChars int
	VerifyBuild         bool
	RestoreFiles        []string
}

func (s *scenario) run(root, bin string, verbose bool) result {
	var directChars int
	for _, f := range s.ReadFiles {
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			continue
		}
		directChars += len(data)
	}
	directChars += s.WriteEstimate

	var refactorChars int
	var buildRan, buildPassed bool

	if len(s.GoRefactorArgs) > 0 {
		cmd := exec.Command(bin, s.GoRefactorArgs...)
		cmd.Dir = root
		start := time.Now()
		out, err := cmd.CombinedOutput()
		elapsed := time.Since(start)

		invocation := "gorefactor " + strings.Join(s.GoRefactorArgs, " ")
		refactorChars = len(invocation) + len(out)

		if verbose {
			fmt.Printf("\n  [%s] %s (%.0fms)\n", s.Name, invocation, float64(elapsed.Milliseconds()))
			if len(out) > 0 {
				preview := string(out)
				if len(preview) > 300 {
					preview = preview[:300] + "…"
				}
				fmt.Printf("  output: %s\n", preview)
			}
			if err != nil {
				fmt.Printf("  error: %v\n", err)
			}
		}

		if s.VerifyBuild {
			buildRan = true
			buildCmd := exec.Command("go", "build", "./...")
			buildCmd.Dir = root
			buildCmd.Env = append(os.Environ(), "GOTOOLCHAIN=auto")
			buildOut, buildErr := buildCmd.CombinedOutput()
			buildPassed = buildErr == nil
			if verbose && !buildPassed {
				fmt.Printf("  build FAILED: %s\n", buildOut)
			}
			for _, f := range s.RestoreFiles {
				restore := exec.Command("git", "checkout", "--", f)
				restore.Dir = root
				restore.Run()
			}
			exec.Command("git", "clean", "-f", "--", "cmd/gorefactor-agent/compact_messages.go").Run()
		}
	} else if s.ExpectedOutputChars > 0 {
		refactorChars = s.ExpectedOutputChars
	}

	return result{
		directChars:   directChars,
		refactorChars: refactorChars,
		buildRan:      buildRan,
		buildPassed:   buildPassed,
	}
}
