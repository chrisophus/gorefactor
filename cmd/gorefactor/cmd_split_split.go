package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

var splitFlags = map[string]bool{
	"--max":     true,
	"--dry-run": false,
	"--json":    false,
	"--gate":    false,
}

func init() {
	registerCommand(Command{
		Name:        "split",
		Description: "Auto-split a Go file over the line limit into multiple files [--max N] [--dry-run]",
		Usage:       "split <file.go> [--max N] [--dry-run] [--json] [--gate]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       splitFlags,
		Run:         splitCommand,
	})
}

func splitCommand(args []string) error {
	pos, flags := parseFlags(args, splitFlags)
	if len(pos) < 1 {
		return usageErrorf("usage: split <file.go> [--max N] [--dry-run]")
	}
	file := pos[0]
	maxSize := defaultSplitMaxLines
	dryRun := flags["--dry-run"] != ""
	if v, ok := flags["--max"]; ok {
		var n int
		_, _ = fmt.Sscanf(v, "%d", &n)
		if n > 0 {
			maxSize = n
		}
	}

	current, err := fileLineCount(file)
	if err != nil {
		return err
	}
	if current <= maxSize {
		fmt.Printf("%s is %d lines (limit %d); no split needed\n", file, current, maxSize)
		return nil
	}

	decls, err := parseSplitDecls(file)
	if err != nil {
		return err
	}
	groups := groupSplitDecls(decls)

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].totalLines() > groups[j].totalLines()
	})

	stem := strings.TrimSuffix(filepath.Base(file), ".go")
	dir := filepath.Dir(file)

	remaining := current
	moves := [][2]string{}
	usedKeys := map[string]bool{}

	for _, g := range groups {
		if remaining <= maxSize {
			break
		}
		if len(g.decls) == 0 {
			continue
		}
		dest := destFileFor(dir, stem, g, usedKeys)
		for _, d := range g.decls {
			moves = append(moves, [2]string{d.targetName(), dest})
		}
		remaining -= g.totalLines() + len(g.decls)
	}

	if len(moves) == 0 {
		fmt.Printf("%s is %d lines (limit %d); no candidate groups to move\n", file, current, maxSize)
		return nil
	}

	fmt.Printf("Plan: split %s (%d lines) into %d move operations\n", file, current, len(moves))
	for _, m := range moves {
		fmt.Printf("  %s -> %s\n", m[0], m[1])
	}
	if dryRun {
		return nil
	}

	affected := []string{file}
	seenDest := map[string]bool{file: true}
	for _, m := range moves {
		if !seenDest[m[1]] {
			seenDest[m[1]] = true
			affected = append(affected, m[1])
		}
	}
	mu := &mutation{op: "split", file: file, files: affected}
	mu.setCommonFlags(flags)
	mu.dryRun = false // split's --dry-run prints the plan above instead

	return mu.run(func() (string, error) {
		orch := orchestrator.NewOrchestrator()
		orch.SkipSnapshot = true
		plan := &orchestrator.RefactoringPlan{
			Version:    "1.0",
			Name:       "split-" + stem,
			Operations: []*orchestrator.RefactoringOperation{},
		}
		for _, m := range moves {
			target := splitTargetFromName(m[0])
			op := &orchestrator.RefactoringOperation{
				Type:   "move_method",
				File:   file,
				Target: target,
				Parameters: map[string]interface{}{
					"newFile": m[1],
				},
			}
			plan.Operations = append(plan.Operations, op)
		}
		if err := orch.RegisterPlan(plan); err != nil {
			return "", err
		}
		result, err := orch.ExecutePlan(plan.Name)
		if err != nil {
			return "", err
		}
		applied := 0
		for _, op := range result.Operations {
			if op.Applied {
				applied++
			}
		}
		return fmt.Sprintf("Applied %d/%d operations", applied, len(plan.Operations)), nil
	})
}

func splitTargetFromName(s string) *orchestrator.TargetSpecification {
	if i := strings.Index(s, ":"); i >= 0 {
		return &orchestrator.TargetSpecification{
			ReceiverType: s[:i],
			MethodName:   s[i+1:],
		}
	}
	return &orchestrator.TargetSpecification{FunctionName: s}
}
