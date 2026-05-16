package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gorefactor/analyzer"
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
				fmt.Sscanf(args[i+1], "%d", &n)
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
	}
	if dups := checkDuplicates(root); len(dups) > 0 {
		issues = append(issues, dups...)
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
	var files []string
	err := filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			name := fi.Name()
			if name == "vendor" || name == ".git" || name == ".gorefactor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func checkFileSize(file string, maxSize int) []lintIssue {
	issue, err := analyzer.AnalyzeFileSize(file, maxSize)
	if err != nil || !issue.IsOversized {
		return nil
	}
	sev := "warning"
	if issue.OverageSize > maxSize/2 {
		sev = "error"
	}
	return []lintIssue{{
		File:       file,
		Rule:       "file-size",
		Severity:   sev,
		Message:    fmt.Sprintf("%d lines (limit %d, over by %d)", issue.LineCount, issue.MaxRecommended, issue.OverageSize),
		AutoFix:    "split file",
		AutoFixCmd: fmt.Sprintf("gorefactor split %s --max %d", file, maxSize),
	}}
}

func checkDuplicates(root string) []lintIssue {
	result, err := analyzer.AnalyzeCrossFile(root)
	if err != nil || result == nil {
		return nil
	}
	var out []lintIssue
	for _, d := range result.DuplicateBlocks {
		if d.ImpactScore < 5 {
			continue
		}
		locs := make([]string, 0, len(d.Locations))
		for _, l := range d.Locations {
			locs = append(locs, fmt.Sprintf("%s:%d-%d", l.File, l.StartLine, l.EndLine))
		}
		out = append(out, lintIssue{
			File:     d.Locations[0].File,
			Rule:     "duplicate-block",
			Severity: "warning",
			Message:  fmt.Sprintf("%d-stmt block duplicated in %d places (impact %d): %s", d.StatementCount, len(d.Locations), d.ImpactScore, strings.Join(locs, ", ")),
		})
	}
	return out
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
		}
	}
	return
}
