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

	// Validate rename if --strict flag is set
	if strict {
		pkgDir := filepath.Dir(file)
		validator, err := analyzer.NewExportedRenameValidator(pkgDir)
		if err != nil {
			return m.fail(fmt.Errorf("failed to initialize validator: %w", err))
		}

		validation, err := validator.ValidateRename(oldName, newName)
		if err != nil {
			return m.fail(fmt.Errorf("validation failed: %w", err))
		}

		// Print validation report
		fmt.Println(validation.SafetyReport(oldName, newName))

		if !validation.SafeToRename {
			return m.fail(fmt.Errorf("rename is not safe; review warnings above"))
		}

		if validation.IsExported && len(validation.Warnings) > 0 {
			fmt.Println("WARNING: Symbol is exported; this rename may break external packages")
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
