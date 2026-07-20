package main

import (
	"fmt"
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
	cmd := exec.Command("go", verb, target)
	cmd.Dir = dir
	cmd.Env = analyzer.SanitizedGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("go %s %s (in %s):\n%s", verb, target, dir, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
