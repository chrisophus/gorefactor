package main

import (
	"fmt"
	"gorefactor/orchestrator"
	"path/filepath"
	"sort"
	"strings"
)

func splitCommand(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: split <file.go> [--max N] [--dry-run]")
	}
	file := args[0]
	maxSize := defaultSplitMaxLines
	dryRun := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--max":
			if i+1 < len(args) {
				var n int
				fmt.Sscanf(args[i+1], "%d", &n)
				if n > 0 {
					maxSize = n
				}
				i++
			}
		case "--dry-run":
			dryRun = true
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

	orch := orchestrator.NewOrchestrator()
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
	orch.RegisterPlan(plan)
	result, err := orch.ExecutePlan(plan.Name)
	if err != nil {
		return err
	}
	applied := 0
	for _, op := range result.Operations {
		if op.Applied {
			applied++
		}
	}
	fmt.Printf("Applied %d/%d operations\n", applied, len(plan.Operations))
	return nil
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
