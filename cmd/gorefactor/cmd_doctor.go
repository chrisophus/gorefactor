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
		Description: "Aggregate health gate: lint + golangci-lint + build + test. Exits non-zero on failure. [--json]",
		Usage:       "doctor [dir] [--json]",
		MinArgs:     0,
		MaxArgs:     1,
		Flags:       map[string]bool{"--json": false},
		Run:         doctorCommand,
	})

}

// 1. structural lint

// 2. build

// 3. test

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

	files, err := collectGoFiles(root, analyzer.DefaultWalkOptions())
	if err != nil {
		return err
	}
	// Use the same per-file size limit the lint command applies, so doctor and
	// lint never disagree. effectiveMaxSizeForFile relaxes the limit for
	// _test.go files; a flat limit here previously made doctor report test
	// files as errors that lint correctly passed.
	sizeCtx := LintContext{MaxSize: defaultSplitMaxLines}
	var issues []lintIssue
	for _, f := range files {
		issues = append(issues, checkFileSize(f, effectiveMaxSizeForFile(f, sizeCtx))...)
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

	gciOK := true
	gciInfo := "skipped (golangci-lint not installed or no config)"
	if golangciLintAvailable(root) {
		gci := golangciLintRule{}.Run(LintContext{Root: root, WalkOpts: walk})
		gciOK = len(gci) == 0
		gciInfo = "ok"
		if !gciOK {
			gciInfo = fmt.Sprintf("%d issue(s)", len(gci))
		}
	}
	stages = append(stages, stage{name: "golangci", ok: gciOK, info: gciInfo})

	buildOut, err := exec.Command("go", "build", "./...").CombinedOutput()
	stages = append(stages, stage{
		name: "build",
		ok:   err == nil,
		info: trimOutput(buildOut),
	})

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
