package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/orchestrator"
)

func renameCommand(args []string) error {
	pos, flags := parseFlags(args, renameFlags)
	if len(pos) < 3 {
		return usageErrorf("usage: rename <file> <oldname> <newname> [--strict]")
	}
	file := pos[0]
	oldName := pos[1]
	newName := pos[2]
	strict := flags["--strict"] != ""
	m := &mutation{op: "rename", file: file, files: packageGoFiles(file)}
	m.setCommonFlags(flags)

	// With --strict, run the advisory rename analysis first. It blocks only
	// on definite problems; advisory hints are printed for the user to judge
	// (the analysis is name-match-only and never claims safety).
	if strict {
		pkgDir := filepath.Dir(file)
		advisor, err := analyzer.NewRenameAdvisor(pkgDir)
		if err != nil {
			return m.fail(fmt.Errorf("failed to initialize rename advisor: %w", err))
		}

		hints, err := advisor.AdviseRename(oldName, newName)
		if err != nil {
			return m.fail(fmt.Errorf("rename analysis failed: %w", err))
		}

		fmt.Println(hints.Report(oldName, newName))

		if hints.HasBlocking() {
			return m.fail(fmt.Errorf("rename has definite problems; review blocking hints above"))
		}
	}

	return m.run(func() (string, error) {
		err := runPlanOps("rename", []*orchestrator.RefactoringOperation{{
			Type:   "rename_declaration",
			File:   file,
			Target: &orchestrator.TargetSpecification{FunctionName: oldName},
			Parameters: map[string]interface{}{
				"newName": newName,
			},
		}})
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				_, all, derr := fileDecls(file)
				if derr != nil {
					all = nil
				}
				return "", notFoundError(
					fmt.Sprintf("symbol %q not found in package of %s", oldName, file),
					oldName, all)
			}
			return "", err
		}
		return fmt.Sprintf("Renamed %s -> %s in %s and dependent files", oldName, newName, file), nil
	})
}
