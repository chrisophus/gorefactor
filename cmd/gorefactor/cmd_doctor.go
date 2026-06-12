package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

func init() {
	registerCommand(Command{
		Name:        "doctor",
		Description: "Aggregate health gate: lint + build + test. Exits non-zero on failure. [--json]",
		Usage:       "doctor [dir] [--json]",
		MinArgs:     0,
		MaxArgs:     1,
		Flags:       map[string]bool{"--json": false},
		Run:         doctorCommand,
	})
}

// doctorCommand runs a fast structural+build+test sweep as a final
// gate after a refactor batch. It returns non-zero (exit code 4) on any
// failure so it can be wired into a pre-commit hook or CI step. Output is
// human-readable; pass --json for machine-parseable.
func doctorCommand(args []string) error {
	root := "."
	jsonOut := false
	for _, a := range args {
		switch {
		case a == "--json":
			jsonOut = true
		case !strings.HasPrefix(a, "--"):
			root = a
		}
	}

	type stage struct {
		name string
		ok   bool
		info string
	}
	var stages []stage

	// 1. structural lint
	files, err := collectGoFiles(root, analyzer.DefaultWalkOptions())
	if err != nil {
		return err
	}
	var issues []lintIssue
	for _, f := range files {
		issues = append(issues, checkFileSize(f, defaultSplitMaxLines)...)
	}
	walk := analyzer.DefaultWalkOptions()
	if dups := checkDuplicates(root, walk); len(dups) > 0 {
		issues = append(issues, dups...)
	}
	if untested := checkUntestedPackages(root, walk); len(untested) > 0 {
		issues = append(issues, untested...)
	}
	errCount := 0
	for _, iss := range issues {
		if iss.Severity == "error" {
			errCount++
		}
	}
	stages = append(stages, stage{
		name: "lint",
		ok:   errCount == 0,
		info: fmt.Sprintf("%d issue(s), %d error(s)", len(issues), errCount),
	})

	// 2. build
	buildOut, err := exec.Command("go", "build", "./...").CombinedOutput()
	stages = append(stages, stage{
		name: "build",
		ok:   err == nil,
		info: trimOutput(buildOut),
	})

	// 3. test
	testOut, err := exec.Command("go", "test", "./...").CombinedOutput()
	stages = append(stages, stage{
		name: "test",
		ok:   err == nil,
		info: trimOutput(testOut),
	})

	if jsonOut {
		fmt.Print("{\"stages\":[")
		for i, s := range stages {
			if i > 0 {
				fmt.Print(",")
			}
			fmt.Printf("{\"name\":%q,\"ok\":%v,\"info\":%q}", s.name, s.ok, s.info)
		}
		fmt.Println("]}")
	} else {
		fmt.Println("gorefactor doctor")
		for _, s := range stages {
			status := "PASS"
			if !s.ok {
				status = "FAIL"
			}
			fmt.Printf("  [%s] %-6s %s\n", status, s.name, s.info)
		}
	}

	for _, s := range stages {
		if !s.ok {
			return gateErrorf("doctor: %s failed", s.name)
		}
	}
	return nil
}

func trimOutput(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	if s == "" {
		return "ok"
	}
	return s
}
