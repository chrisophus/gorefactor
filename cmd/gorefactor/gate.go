package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

// goGate runs `go <verb> <target>` in dir with a git-sanitized environment.
// It is the single build/test gate primitive shared by the mutation runner,
// the txn batch, `lint --fix --verify`, and `doctor`. It returns the combined
// output and, on non-zero exit, a descriptive error including that output.
func goGate(dir, verb, target string) (string, error) {
	if dir == "" {
		dir = "."
	}
	args := []string{verb, target}
	if verb == "build" {
		// Send executables to a throwaway dir: `go build ./...` run inside a
		// directory containing exactly one main package writes the binary
		// into the working directory, littering package dirs (the exact
		// defect the tracked-artifact sensor exists to catch). -o with a
		// trailing-slash directory works for any number of main packages.
		tmp, terr := os.MkdirTemp("", "gorefactor-gate-*")
		if terr == nil {
			defer func() { _ = os.RemoveAll(tmp) }()

			args = []string{verb, "-o", tmp + string(os.PathSeparator), target}
		}
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = analyzer.SanitizedGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("go %s %s (in %s):\n%s", verb, target, dir, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
