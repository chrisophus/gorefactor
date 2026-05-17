package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gorefactor/analyzer"
	"gorefactor/orchestrator"
)

func readContentArg(args []string, idx int) (string, error) {
	if idx < len(args) && args[idx] != "-" {
		return args[idx], nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func createCommand(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: create <path> [content] (else stdin)")
	}
	path := args[0]
	content, err := readContentArg(args, 1)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("file already exists: %s (use replace or insert)", path)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	if strings.HasSuffix(path, ".go") {
		if err := orchestrator.FormatImports(path); err != nil {
			fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", path, err)
		}
	}
	fmt.Printf("Created %s\n", path)
	return nil
}

func parseLocSpec(s string) (*orchestrator.InsertionLocation, error) {
	switch {
	case s == "at-end":
		return &orchestrator.InsertionLocation{Type: "at_end"}, nil
	case s == "at-beginning":
		return &orchestrator.InsertionLocation{Type: "at_beginning"}, nil
	case strings.HasPrefix(s, "before:"):
		return &orchestrator.InsertionLocation{Type: "before_function", FunctionName: strings.TrimPrefix(s, "before:")}, nil
	case strings.HasPrefix(s, "after:"):
		return &orchestrator.InsertionLocation{Type: "after_function", FunctionName: strings.TrimPrefix(s, "after:")}, nil
	case strings.HasPrefix(s, "inside:"):
		return &orchestrator.InsertionLocation{Type: "inside_function", FunctionName: strings.TrimPrefix(s, "inside:")}, nil
	}
	return nil, fmt.Errorf("unknown location %q; use at-end | at-beginning | before:Func | after:Func | inside:Func", s)
}

func insertCommand(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: insert <file> <at-end|at-beginning|before:Func|after:Func|inside:Func> [content] (else stdin)")
	}
	file := args[0]
	loc, err := parseLocSpec(args[1])
	if err != nil {
		return err
	}
	content, err := readContentArg(args, 2)
	if err != nil {
		return err
	}
	ci := orchestrator.NewCodeInserter()
	ins, err := ci.InsertCode(file, loc, content)
	if err != nil {
		return err
	}
	if err := orchestrator.FormatImports(file); err != nil {
		fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
	}
	fmt.Printf("Inserted into %s at lines %d-%d\n", file, ins.StartLine, ins.EndLine)
	return nil
}

func replaceCommand(args []string) error {
	if len(args) < 4 {
		return fmt.Errorf("usage: replace <file> <funcname-or-Receiver:Method> <pattern> <replacement>")
	}
	file := args[0]
	loc, err := parseFuncLocator(args[1])
	if err != nil {
		return err
	}
	pattern := args[2]
	replacement := args[3]
	ci := orchestrator.NewCodeInserter()
	ins, err := ci.ReplaceCodeBlock(file, loc, pattern, replacement)
	if err != nil {
		return err
	}
	if err := orchestrator.FormatImports(file); err != nil {
		fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
	}
	fmt.Printf("Replaced in %s at lines %d-%d\n", file, ins.StartLine, ins.EndLine)
	return nil
}

func parseFuncLocator(s string) (*orchestrator.InsertionLocation, error) {
	if i := strings.Index(s, ":"); i >= 0 {
		return &orchestrator.InsertionLocation{
			ReceiverType: s[:i],
			MethodName:   s[i+1:],
		}, nil
	}
	return &orchestrator.InsertionLocation{FunctionName: s}, nil
}

func deleteCommand(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: delete <file> <funcname-or-Receiver:Method>")
	}
	file := args[0]
	target := parseTargetSpec(args[1])
	plan := &orchestrator.RefactoringPlan{
		Version: "1.0",
		Name:    "delete-decl",
		Operations: []*orchestrator.RefactoringOperation{{
			Type:   "delete_declaration",
			File:   file,
			Target: target,
		}},
	}
	orch := orchestrator.NewOrchestrator()
	orch.RegisterPlan(plan)
	_, err := orch.ExecutePlan(plan.Name)
	if err != nil {
		return err
	}
	fmt.Printf("Deleted %s from %s\n", args[1], file)
	return nil
}

func renameCommand(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: rename <file> <oldname> <newname> [--strict]")
	}
	file := args[0]
	oldName := args[1]
	newName := args[2]
	strict := false

	for i := 3; i < len(args); i++ {
		if args[i] == "--strict" {
			strict = true
		}
	}

	// Validate rename if --strict flag is set
	if strict {
		pkgDir := filepath.Dir(file)
		validator, err := analyzer.NewExportedRenameValidator(pkgDir)
		if err != nil {
			return fmt.Errorf("failed to initialize validator: %w", err)
		}

		validation, err := validator.ValidateRename(oldName, newName)
		if err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}

		// Print validation report
		fmt.Println(validation.SafetyReport(oldName, newName))

		if !validation.SafeToRename {
			return fmt.Errorf("rename is not safe; review warnings above")
		}

		if validation.IsExported && len(validation.Warnings) > 0 {
			fmt.Println("WARNING: Symbol is exported; this rename may break external packages")
		}
	}

	plan := &orchestrator.RefactoringPlan{
		Version: "1.0",
		Name:    "rename",
		Operations: []*orchestrator.RefactoringOperation{{
			Type:   "rename_declaration",
			File:   file,
			Target: &orchestrator.TargetSpecification{FunctionName: oldName},
			Parameters: map[string]interface{}{
				"newName": newName,
			},
		}},
	}
	orch := orchestrator.NewOrchestrator()
	orch.RegisterPlan(plan)
	_, err := orch.ExecutePlan(plan.Name)
	if err != nil {
		return err
	}
	fmt.Printf("Renamed %s -> %s in %s and dependent files\n", oldName, newName, file)
	return nil
}

func parseTargetSpec(s string) *orchestrator.TargetSpecification {
	if i := strings.Index(s, ":"); i >= 0 {
		return &orchestrator.TargetSpecification{
			ReceiverType: s[:i],
			MethodName:   s[i+1:],
		}
	}
	return &orchestrator.TargetSpecification{FunctionName: s}
}
