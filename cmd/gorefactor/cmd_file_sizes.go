package main

import (
	"encoding/json"
	"fmt"
	"github.com/chrisophus/gorefactor/analyzer"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func analyzeFileSizes(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: analyze-file-sizes <directory> [--max-size N] [--format json|text]")
	}

	directory := args[0]
	maxSize := 300
	format := "text"

	// Parse optional arguments
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--max-size":
			if i+1 < len(args) {
				size, err := strconv.Atoi(args[i+1])
				if err != nil {
					return fmt.Errorf("invalid max-size: %w", err)
				}
				maxSize = size
				i++
			}
		case "--format":
			if i+1 < len(args) {
				format = args[i+1]
				i++
			}
		}
	}

	// Find all Go files
	files, err := findGoFiles(directory)
	if err != nil {
		return fmt.Errorf("failed to find Go files: %w", err)
	}

	if len(files) == 0 {
		fmt.Println("No Go files found in directory")
		return nil
	}

	// Analyze each file
	var issues []*analyzer.FileSizeIssue
	for _, file := range files {
		issue, err := analyzer.AnalyzeFileSize(file, maxSize)
		if err != nil {
			// Log but don't fail
			fmt.Fprintf(os.Stderr, "Warning: failed to analyze %s: %v\n", file, err)
			continue
		}
		issues = append(issues, issue)
	}

	// Output results
	if format == "json" {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(issues)
	}

	// Text format (linter-style)
	oversized := 0
	for _, issue := range issues {
		if issue.IsOversized {
			oversized++
			fmt.Printf("%s: %d lines (max: %d) - %d lines over limit\n",
				issue.FilePath, issue.LineCount, issue.MaxRecommended, issue.OverageSize)

			// Show extraction hints
			if len(issue.ExtractionHints) > 0 {
				fmt.Println("  Extraction candidates:")
				for _, hint := range issue.ExtractionHints {
					fmt.Printf("    - %s (lines %d-%d, %d lines, complexity %d, priority %d/10)\n",
						hint.FunctionName, hint.StartLine, hint.EndLine, hint.LineCount, hint.Complexity, hint.Priority)
				}
			}
		}
	}

	// Summary
	fmt.Printf("\nSummary: %d/%d files exceed %d lines\n", oversized, len(issues), maxSize)

	if oversized > 0 {
		return fmt.Errorf("found %d oversized file(s)", oversized)
	}

	return nil
}

func findGoFiles(directory string) ([]string, error) {
	var files []string
	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
