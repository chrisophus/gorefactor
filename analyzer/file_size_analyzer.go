package analyzer

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
)

const DefaultMaxFileSize = 300

// FileSizeIssue represents a finding about file size
type FileSizeIssue struct {
	FilePath        string `json:"filePath"`
	LineCount       int    `json:"lineCount"`
	MaxRecommended  int    `json:"maxRecommended"`
	IsOversized     bool   `json:"isOversized"`
	OverageSize     int    `json:"overageSize"`
	ExtractionHints []*ExtractionHint `json:"extractionHints"`
}

// ExtractionHint suggests a function that could be extracted to reduce file size
type ExtractionHint struct {
	FunctionName  string `json:"functionName"`
	StartLine     int    `json:"startLine"`
	EndLine       int    `json:"endLine"`
	LineCount     int    `json:"lineCount"`
	Complexity    int    `json:"complexity"`
	Priority      int    `json:"priority"` // 1-10, higher = more important to extract
}

// AnalyzeFileSize analyzes whether a file exceeds recommended size and suggests extractions
func AnalyzeFileSize(filePath string, maxSize int) (*FileSizeIssue, error) {
	if maxSize == 0 {
		maxSize = DefaultMaxFileSize
	}

	// Count lines in file
	lineCount, err := countLines(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to count lines: %w", err)
	}

	issue := &FileSizeIssue{
		FilePath:       filePath,
		LineCount:      lineCount,
		MaxRecommended: maxSize,
		IsOversized:    lineCount > maxSize,
	}

	if issue.IsOversized {
		issue.OverageSize = lineCount - maxSize
		// Get extraction hints for oversized files
		hints, err := getExtractionHints(filePath)
		if err == nil {
			issue.ExtractionHints = hints
		}
	}

	return issue, nil
}

// countLines counts the number of lines in a file
func countLines(filePath string) (int, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		count++
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return count, nil
}

// getExtractionHints returns functions that could be extracted to reduce file size
func getExtractionHints(filePath string) ([]*ExtractionHint, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, 0)
	if err != nil {
		return nil, err
	}

	var hints []*ExtractionHint

	// Analyze each function in the file
	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		// Skip methods for now, focus on top-level functions
		if fn.Recv != nil {
			continue
		}

		startLine := fset.Position(fn.Pos()).Line
		endLine := fset.Position(fn.End()).Line
		lineCount := endLine - startLine + 1

		// Calculate complexity and priority
		complexity := calculateFunctionComplexity(fn)
		priority := calculateExtractionPriority(lineCount, complexity)

		// Only include functions with good extraction potential
		if lineCount > 20 && priority >= 5 {
			hints = append(hints, &ExtractionHint{
				FunctionName: fn.Name.Name,
				StartLine:    startLine,
				EndLine:      endLine,
				LineCount:    lineCount,
				Complexity:   complexity,
				Priority:     priority,
			})
		}
	}

	// Sort by priority (highest first)
	for i := 0; i < len(hints)-1; i++ {
		for j := i + 1; j < len(hints); j++ {
			if hints[j].Priority > hints[i].Priority {
				hints[i], hints[j] = hints[j], hints[i]
			}
		}
	}

	return hints, nil
}

// calculateFunctionComplexity calculates cyclomatic complexity of a function
func calculateFunctionComplexity(fn *ast.FuncDecl) int {
	if fn.Body == nil {
		return 0
	}

	complexity := 1 // Base complexity
	countComplexity(fn.Body, &complexity)
	return complexity
}

// countComplexity recursively counts complexity points
func countComplexity(node ast.Node, count *int) {
	switch n := node.(type) {
	case *ast.IfStmt:
		*count++
		if n.Body != nil {
			countComplexity(n.Body, count)
		}
		if n.Else != nil {
			countComplexity(n.Else, count)
		}
	case *ast.ForStmt, *ast.RangeStmt:
		*count++
		if n, ok := node.(*ast.ForStmt); ok && n.Body != nil {
			countComplexity(n.Body, count)
		}
		if n, ok := node.(*ast.RangeStmt); ok && n.Body != nil {
			countComplexity(n.Body, count)
		}
	case *ast.SwitchStmt:
		*count += 2 // Higher penalty for switch
		if s, ok := node.(*ast.SwitchStmt); ok && s.Body != nil {
			for _, stmt := range s.Body.List {
				countComplexity(stmt, count)
			}
		}
	case *ast.SelectStmt:
		*count += 2
		if s, ok := node.(*ast.SelectStmt); ok && s.Body != nil {
			for _, stmt := range s.Body.List {
				countComplexity(stmt, count)
			}
		}
	case *ast.BlockStmt:
		if n != nil {
			for _, stmt := range n.List {
				countComplexity(stmt, count)
			}
		}
	}
}

// calculateExtractionPriority determines how important it is to extract a function
// Returns 1-10 scale where 10 is highest priority
func calculateExtractionPriority(lineCount int, complexity int) int {
	priority := 0

	// Line count factor (size is the main concern)
	if lineCount > 100 {
		priority += 10
	} else if lineCount > 75 {
		priority += 8
	} else if lineCount > 50 {
		priority += 6
	} else if lineCount > 30 {
		priority += 4
	} else if lineCount >= 20 {
		priority += 2
	}

	// Complexity factor
	if complexity > 15 {
		priority += 3
	} else if complexity > 10 {
		priority += 2
	} else if complexity > 5 {
		priority += 1
	}

	// Cap at 10
	if priority > 10 {
		priority = 10
	}

	return priority
}
