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
	lintStage, err := doctorLintStage(root)
	if err != nil {
		return err
	}
	stages := []doctorStage{
		lintStage,
		doctorGolangciStage(root),
		doctorGoStage(root, "build"),
		doctorGoStage(root, "test"),
	}
	reportDoctorStages(stages, jsonOut)
	for _, s := range stages {
		if !s.ok {
			return gateErrorf("doctor: %s failed", s.name)
		}
	}
	return nil

}

type doctorStage struct {
	name string
	ok   bool
	info string
}

func doctorLintStage(root string) (doctorStage, error) {
	files, err := collectGoFiles(root, analyzer.DefaultWalkOptions())
	if err != nil {
		return doctorStage{}, err
	}

	sizeCtx := LintContext{MaxSize: defaultSplitMaxLines}
	var issues []lintIssue
	for _, f := range files {
		issues = append(issues, checkFileSize(f, effectiveMaxSizeForFile(f, sizeCtx))...)
	}
	walk := analyzer.DefaultWalkOptions()
	issues = append(issues, checkDuplicates(root, walk)...)
	issues = append(issues, checkUntestedPackages(root, walk)...)
	errCount := 0
	for _, iss := range issues {
		if iss.Severity == "error" {
			errCount++
		}
	}
	return doctorStage{
		name: "lint",
		ok:   errCount == 0,
		info: fmt.Sprintf("%d issue(s), %d error(s)", len(issues), errCount),
	}, nil
}

func doctorGolangciStage(root string) doctorStage {
	if !golangciLintAvailable(root) {
		return doctorStage{name: "golangci", ok: true, info: "skipped (golangci-lint not installed or no config)"}
	}
	gci := golangciLintRule{}.Run(LintContext{Root: root, WalkOpts: analyzer.DefaultWalkOptions()})
	if len(gci) == 0 {
		return doctorStage{name: "golangci", ok: true, info: "ok"}
	}
	return doctorStage{name: "golangci", ok: false, info: fmt.Sprintf("%d issue(s)", len(gci))}
}

func doctorGoStage(root, verb string) doctorStage {
	cmd := exec.Command("go", verb, "./...")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return doctorStage{name: verb, ok: err == nil, info: trimOutput(out)}
}

func reportDoctorStages(stages []doctorStage, jsonOut bool) {
	if jsonOut {
		fmt.Print("{\"stages\":[")
		for i, s := range stages {
			if i > 0 {
				fmt.Print(",")
			}
			fmt.Printf("{\"name\":%q,\"ok\":%v,\"info\":%q}", s.name, s.ok, s.info)
		}
		fmt.Println("]}")
		return
	}
	fmt.Println("gorefactor doctor")
	for _, s := range stages {
		status := "PASS"
		if !s.ok {
			status = "FAIL"
		}
		fmt.Printf("  [%s] %-6s %s\n", status, s.name, s.info)
	}
}
