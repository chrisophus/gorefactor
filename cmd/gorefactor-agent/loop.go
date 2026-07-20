package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/orchestrator"
)

// Config holds one driver run's parameters.
type Config struct {
	Spec       string    // purified refactoring spec (from crucible, etc.)
	Dir        string    // target Go module directory
	MaxIter    int       // attempt / tool-step cap
	AllowDirty bool      // skip the clean-worktree precondition
	Verbose    bool      // echo the raw model response each iteration
	Budget     int       // token budget (prompt+completion); 0 = unlimited
	Out        io.Writer // progress sink
}

// requireCleanWorktree enforces total, safe rollback: if the tree is
// clean, `reset --hard` + `clean -fd` perfectly undoes any attempt.
func requireCleanWorktree(dir string) error {
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").CombinedOutput(); err != nil {
		return fmt.Errorf("target %s is not a git work tree (needed for safe rollback): %s",
			dir, strings.TrimSpace(string(out)))
	}
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").CombinedOutput()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		return fmt.Errorf("working tree is dirty; commit/stash first or pass -allow-dirty")
	}
	return nil
}

func rollback(dir string, out io.Writer) {
	if err := exec.Command("git", "-C", dir, "reset", "--hard").Run(); err != nil {
		fmt.Fprintf(out, "  ! rollback reset failed: %v\n", err)
	}
	if err := exec.Command("git", "-C", dir, "clean", "-fd").Run(); err != nil {
		fmt.Fprintf(out, "  ! rollback clean failed: %v\n", err)
	}
}

// runGate is doctor's behavioural core: build then test. A green gate
// is the only thing that lets a refactor stand.
func runGate(dir string) (bool, string) {
	if out, err := runIn(dir, "go", "build", "./..."); err != nil {
		return false, "go build ./...\n" + out
	}
	if out, err := runIn(dir, "go", "test", "./..."); err != nil {
		return false, "go test ./...\n" + out
	}
	// Third leg of the gate (design plan): no new error-severity doctor
	// findings vs HEAD. Advisory mode reports them in the success output
	// instead of blocking; hard mode fails the gate.
	blocking, advisory := runDoctorGate(dir, false)
	if blocking != "" {
		return false, "doctor gate (new findings vs HEAD):\n" + blocking
	}
	return true, advisory

}

// runInWithStdin runs a command with the given string piped to stdin.
func runInWithStdin(dir, stdin, name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	c.Dir = dir
	env := append(analyzer.SanitizedGitEnv(), "GOTOOLCHAIN=auto")

	if v := os.Getenv("GOTMPDIR"); v != "" {
		env = append(env, "GOTMPDIR="+v)
	}
	if v := os.Getenv("GOCACHE"); v != "" {
		env = append(env, "GOCACHE="+v)
	}
	c.Env = env
	c.Stdin = strings.NewReader(stdin)
	b, err := c.CombinedOutput()
	return trim(string(b), 1500), err
}

func runIn(dir, name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	c.Dir = dir
	env := append(analyzer.SanitizedGitEnv(), "GOTOOLCHAIN=auto")

	// If /tmp is noexec (common in some container environments), the caller can
	// set GOTMPDIR to a directory that allows execution. We honour it here so
	// go test can write and execute test binaries. GOCACHE follows suit.
	if v := os.Getenv("GOTMPDIR"); v != "" {
		env = append(env, "GOTMPDIR="+v)
	}
	if v := os.Getenv("GOCACHE"); v != "" {
		env = append(env, "GOCACHE="+v)
	}
	c.Env = env
	b, err := c.CombinedOutput()
	return trim(string(b), 1500), err
}

func execErrors(res *orchestrator.ExecutionResult, err error) string {
	var b strings.Builder
	if err != nil {
		fmt.Fprintf(&b, "%v\n", err)
	}
	if res != nil {
		for _, e := range res.Errors {
			fmt.Fprintf(&b, "- %s\n", e)
		}
	}
	return b.String()
}

func trim(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	// Head+tail: preserve first error context (head) and final state (tail).
	head := max * 2 / 3
	tail := max - head - 30
	if tail < 60 {
		return s[:max] + "\n…(truncated)"
	}
	omitted := len(s) - head - tail
	return s[:head] + fmt.Sprintf("\n…(%d bytes omitted)…\n", omitted) + s[len(s)-tail:]
}
