package main

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/parser"
)

// inspectCommand prints a human-readable one-page summary of a file:
// line count vs. configured limit, every top-level declaration with its
// size, any lint issues that fire on this single file, and the highest
// priority extraction candidates. Designed as the LLM's first stop
// when it inherits an unfamiliar file.
func inspectCommand(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: inspect <file.go> [--max N]")
	}
	file := args[0]
	maxSize := defaultSplitMaxLines
	maxSize = extractBlockL23(args, maxSize)

	info, err := parser.ParseFile(file)
	if err != nil {
		return err
	}
	lines, err := fileLineCount(file)
	if err != nil {
		return err
	}

	fmt.Printf("File: %s\n", filepath.Clean(file))
	fmt.Printf("Package: %s\n", info.Package)
	status := "ok"
	if lines > maxSize {
		status = fmt.Sprintf("OVER by %d", lines-maxSize)
	}
	fmt.Printf("Lines: %d / %d (%s)\n\n", lines, maxSize, status)

	complexities, _ := analyzer.FileFunctionComplexities(file)
	cxByName := make(map[string]int, len(complexities))
	for _, c := range complexities {
		cxByName[c.Name] = c.Complexity
	}
	type declRow struct {
		kind   string
		name   string
		lines  int
		startL int
		cx     int
	}
	var rows []declRow
	for _, fn := range info.Functions {
		rows = append(rows, declRow{"func", fn.Name, fn.EndLine - fn.StartLine + 1, fn.StartLine, cxByName[fn.Name]})
	}
	for _, m := range info.Methods {
		rows = append(rows, declRow{"method", m.Receiver + "." + m.Name, m.EndLine - m.StartLine + 1, m.StartLine, cxByName[m.Name]})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].startL < rows[j].startL })
	fmt.Printf("Declarations (%d):\n", len(rows))
	for _, r := range rows {
		fmt.Printf("  %-7s  %-40s  %4d lines  cx=%2d  (L%d)\n", r.kind, r.name, r.lines, r.cx, r.startL)
	}
	fmt.Println()

	issues := checkFileSize(file, maxSize)
	issues = append(issues, checkExtractable(file, 5, false)...)
	if len(issues) == 0 {
		fmt.Println("Lint issues: none")
		return nil
	}
	fmt.Printf("Lint issues (%d):\n", len(issues))
	for _, iss := range issues {
		fmt.Printf("  [%s] %s: %s\n", iss.Severity, iss.Rule, iss.Message)
	}

	extractBlockL79(file, maxSize)
	return nil
}

func extractBlockL79(file string, maxSize int) {
	if issue, err := analyzer.AnalyzeFileSize(file, maxSize); err == nil && len(issue.ExtractionHints) > 0 {
		shown := 0
		fmt.Println("\nTop extraction candidates:")
		for _, h := range issue.ExtractionHints {
			if shown >= 5 {
				break
			}
			fmt.Printf("  %s (L%d-%d, %d lines, complexity %d, priority %d/10)\n",
				h.FunctionName, h.StartLine, h.EndLine, h.LineCount, h.Complexity, h.Priority)
			shown++
		}
	}
}

func extractBlockL23(args []string, maxSize int) int {
	for i := 1; i < len(args); i++ {
		if args[i] == "--max" && i+1 < len(args) {
			var n int
			_, _ = fmt.Sscanf(args[i+1], "%d", &n)
			if n > 0 {
				maxSize = n
			}
			i++
		}
	}
	return maxSize
}
