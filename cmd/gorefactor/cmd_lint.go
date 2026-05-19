package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

type lintIssue struct {
	File       string `json:"file"`
	Rule       string `json:"rule"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	AutoFix    string `json:"autofix,omitempty"`
	AutoFixCmd string `json:"autofixCmd,omitempty"`
}

func lintCommand(args []string) error {
	root := "."
	maxSize := defaultSplitMaxLines
	fix := false
	jsonOut := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--fix":
			fix = true
		case a == "--json":
			jsonOut = true
		case a == "--max":
			if i+1 < len(args) {
				var n int
				_, _ = fmt.Sscanf(args[i+1], "%d", &n)
				if n > 0 {
					maxSize = n
				}
				i++
			}
		case !strings.HasPrefix(a, "--"):
			root = a
		}
	}

	files, err := collectGoFiles(root)
	if err != nil {
		return err
	}

	var issues []lintIssue
	for _, f := range files {
		issues = append(issues, checkFileSize(f, maxSize)...)
		issues = append(issues, checkExtractable(f, 8)...)
		issues = append(issues, checkSmells(f)...)
	}
	if dups := checkDuplicates(root); len(dups) > 0 {
		issues = append(issues, dups...)
	}
	if dead := checkDeadCode(root); len(dead) > 0 {
		issues = append(issues, dead...)
	}
	if untested := checkUntestedPackages(root); len(untested) > 0 {
		issues = append(issues, untested...)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]interface{}{
			"issues": issues,
			"summary": map[string]int{
				"total": len(issues),
			},
		})
	}

	if len(issues) == 0 {
		fmt.Println("No issues found.")
		return nil
	}

	byRule := map[string]int{}
	for _, iss := range issues {
		byRule[iss.Rule]++
		fmt.Printf("%s [%s] %s: %s", iss.File, iss.Severity, iss.Rule, iss.Message)
		if iss.AutoFix != "" {
			fmt.Printf("  (autofix: %s)", iss.AutoFix)
		}
		fmt.Println()
	}
	fmt.Println()
	fmt.Printf("Summary: %d issue(s)\n", len(issues))
	for rule, n := range byRule {
		fmt.Printf("  %s: %d\n", rule, n)
	}

	if fix {
		applied, failed := applyAutoFixes(issues, maxSize)
		fmt.Printf("\nAuto-fixes: %d applied, %d failed\n", applied, failed)
	}
	return nil
}

func collectGoFiles(root string) ([]string, error) {
	return analyzer.WalkGoFiles(root, analyzer.DefaultWalkOptions())
}

func applyAutoFixes(issues []lintIssue, maxSize int) (applied, failed int) {
	for _, iss := range issues {
		if iss.AutoFixCmd == "" {
			continue
		}
		if iss.Rule == "file-size" {
			if err := splitCommand([]string{iss.File, "--max", fmt.Sprintf("%d", maxSize)}); err != nil {
				fmt.Fprintf(os.Stderr, "fix failed for %s: %v\n", iss.File, err)
				failed++
				continue
			}
			applied++
		} else if iss.Rule == "dead-code" {
			// AutoFixCmd is the full intended command, e.g.
			// "delete <file> <target> --safe"; forward its args
			// verbatim so the --safe caller-check is preserved.
			parts := strings.Fields(iss.AutoFixCmd)
			if len(parts) >= 3 && parts[0] == "delete" {
				if err := deleteCommand(parts[1:]); err != nil {
					fmt.Fprintf(os.Stderr, "fix failed for %s: %v\n", iss.File, err)
					failed++
					continue
				}
				applied++
			}
		}
	}
	return
}
