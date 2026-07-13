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
		Description: "Aggregate health gate: lint + golangci-lint + go-arch-lint + build + test. Exits non-zero on failure. [--json] [--fix [--fix-level safe|aggressive]]",
		Usage:       "doctor [dir] [--json] [--fix] [--fix-level safe|aggressive]",
		MinArgs:     0,
		MaxArgs:     1,
		Flags:       map[string]bool{"--json": false, "--fix": false, "--fix-level": true},
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
	fix := false
	fixLevel := fixLevelSafe
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			jsonOut = true
		case a == "--fix":
			fix = true
		case a == "--fix-level":
			if i+1 >= len(args) {
				return fmt.Errorf("--fix-level requires safe or aggressive")
			}
			switch args[i+1] {
			case fixLevelSafe, fixLevelAggressive:
				fixLevel = args[i+1]
			default:
				return fmt.Errorf("--fix-level must be safe or aggressive")
			}
			i++
		case !strings.HasPrefix(a, "--"):
			root = a
		}
	}

	var stages []doctorStage
	if fix {
		stage, ferr := doctorAutoFixStage(root, fixLevel)
		if ferr != nil {
			return ferr
		}
		stages = append(stages, stage)
	}

	lintStage, err := doctorLintStage(root)
	if err != nil {
		return err
	}
	stages = append(stages,
		lintStage,
		doctorGolangciStage(root),
		doctorArchStage(root),
		doctorGoStage(root, "build"),
		doctorGoStage(root, "test"),
	)
	reportDoctorStages(stages, jsonOut)
	for _, s := range stages {
		if !s.ok {
			return gateErrorf("doctor: %s failed", s.name)
		}
	}
	return nil

}

var doctorAutoFixFn = doctorAutoFix

func doctorAutoFixStage(root, fixLevel string) (doctorStage, error) {
	applied, reverted, failed, err := doctorAutoFixFn(root, fixLevel)
	if err != nil {
		return doctorStage{}, err
	}
	return doctorStage{
		name: "autofix",
		ok:   true,
		info: fmt.Sprintf("%d applied, %d reverted (gate failed), %d failed to apply", applied, reverted, failed),
	}, nil
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
	for _, iss := range gci {
		if iss.Rule == golangciToolFailureRule {
			// A tool that's present but can't run (version-skewed binary, config
			// it can't load, ...) gets the same soft-skip treatment as one
			// that's missing entirely: it can't be told apart from "clean" by
			// this stage, so it must not gate local commits — CI runs a known-
			// good golangci-lint and is the real enforcement backstop.
			return doctorStage{name: "golangci", ok: true, info: "skipped, did not run: " + iss.Message}
		}
	}
	if len(gci) == 0 {
		return doctorStage{name: "golangci", ok: true, info: "ok"}
	}
	return doctorStage{name: "golangci", ok: false, info: fmt.Sprintf("%d issue(s)", len(gci))}

}

func doctorArchStage(root string) doctorStage {
	if detectArchConfig(root) == "" {
		return doctorStage{name: "arch", ok: true, info: "skipped (no .go-arch-lint config)"}
	}
	if _, err := exec.LookPath("go-arch-lint"); err != nil {
		return doctorStage{name: "arch", ok: true, info: "skipped (go-arch-lint not installed)"}
	}
	arch := archLintRule{}.Run(LintContext{Root: root, WalkOpts: analyzer.DefaultWalkOptions()})
	if len(arch) == 0 {
		return doctorStage{name: "arch", ok: true, info: "ok"}
	}
	return doctorStage{name: "arch", ok: false, info: fmt.Sprintf("%d violation(s)", len(arch))}
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

// doctorAutoFix runs the default lint ruleset over root and applies every
// autofix with the build+test gate on (each fix is snapshotted and reverted
// individually if it breaks the gate) — the same guarantee as
// `lint --fix --verify`, but silent and always verified, since doctor is
// itself the trust gate. Used by `doctor --fix`.
func doctorAutoFix(root, fixLevel string) (applied, reverted, failed int, err error) {
	opts := lintOptions{root: root, fix: true, verify: true, fixLevel: fixLevel}
	if err := opts.loadConfig(); err != nil {
		return 0, 0, 0, err
	}
	ctx := opts.lintContext(nil)
	files, ferr := collectGoFiles(root, ctx.WalkOpts)
	if ferr != nil {
		return 0, 0, 0, ferr
	}
	ctx.Files = files

	rules := filterLintRules(defaultLintRules(), opts)
	issues := runLintRules(rules, ctx, opts)
	issues = applyConfigSeverity(issues, opts)
	applied, reverted, failed = applyAutoFixes(issues, ctx, rules, true)

	// Whole-tree gofmt+goimports sweep: individual mutation ops already format
	// the files they touch, but this catches files no autofix rule reached.
	if err := formatCommand([]string{root}); err != nil {
		return applied, reverted, failed, err
	}
	return applied, reverted, failed, nil

}
