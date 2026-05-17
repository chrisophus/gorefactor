package analyzer

import (
	"crypto/md5"
	"fmt"
	"go/ast"
	"path/filepath"
	"regexp"
	"strings"
)

// NormalizeCode removes variable names and formatting for semantic comparison
func NormalizeCode(code string) string {
	// Remove single-line comments
	code = regexp.MustCompile(`//.*`).ReplaceAllString(code, "")
	// Remove multi-line comments (using (?s) flag so . matches newlines)
	code = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(code, "")

	// Remove extra whitespace
	code = regexp.MustCompile(`\s+`).ReplaceAllString(code, " ")

	// Replace variable names with placeholders to find structural duplicates
	// Match identifiers (word characters but not keywords)
	keywords := map[string]bool{
		"if": true, "else": true, "for": true, "range": true, "switch": true, "case": true,
		"default": true, "break": true, "continue": true, "return": true, "var": true,
		"const": true, "func": true, "defer": true, "go": true, "select": true, "chan": true,
		"interface": true, "struct": true, "map": true, "error": true, "nil": true,
		"true": true, "false": true, "iota": true,
	}

	// Simple identifier replacement - preserve structure
	words := strings.Fields(code)
	for i, w := range words {
		// Check if it's an identifier (starts with letter, contains only letters/digits/_)
		if len(w) > 0 && (w[0] >= 'a' && w[0] <= 'z' || w[0] >= 'A' && w[0] <= 'Z' || w[0] == '_') {
			if !keywords[w] && !isBuiltin(w) {
				words[i] = "VAR"
			}
		}
	}
	code = strings.Join(words, " ")

	return strings.TrimSpace(code)
}

// isBuiltin checks if a word is a Go builtin function or type
func isBuiltin(name string) bool {
	builtins := map[string]bool{
		"make": true, "len": true, "cap": true, "new": true, "append": true,
		"copy": true, "close": true, "delete": true, "complex": true, "real": true,
		"imag": true, "panic": true, "recover": true, "print": true, "println": true,
		"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
		"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
		"uintptr": true, "float32": true, "float64": true, "complex64": true, "complex128": true,
		"bool": true, "string": true, "byte": true, "rune": true,
	}
	return builtins[name]
}

// hashCode creates an MD5 hash of normalized code
func hashCode(code string) string {
	hash := md5.Sum([]byte(code))
	return fmt.Sprintf("%x", hash)
}

// extractCodeFromLinesSlice extracts code from pre-split lines (avoids repeated string splitting)
func extractCodeFromLinesSlice(lines []string, startLine, endLine int) string {
	if startLine < 1 || startLine > len(lines) || endLine < startLine || endLine > len(lines) {
		return ""
	}

	// Lines are 1-indexed
	extracted := lines[startLine-1 : endLine]
	return strings.Join(extracted, "\n")
}

// countStatements counts the number of statements in a block
func countStatements(block *ast.BlockStmt) int {
	if block == nil {
		return 0
	}
	return len(block.List)
}

// countControlStructures counts if/for/switch statements
func countControlStructures(node ast.Node) int {
	count := 0
	ast.Inspect(node, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.SelectStmt:
			count++
		}
		return true
	})
	return count
}

// calculateImpactScore computes how valuable it is to fix this duplicate
// Higher score = more impactful
func calculateImpactScore(locationCount, statementCount, complexity int) int {
	// Base score: benefit of each location having the duplicate code
	score := (locationCount - 1) * statementCount

	// Multiply by complexity (more complex = more error-prone to maintain)
	score = score * (1 + complexity)

	// Maximum reasonable score
	if score > 1000 {
		score = 1000
	}

	return score
}

// generateDuplicateRecommendation generates a text recommendation for fixing a duplicate
func generateDuplicateRecommendation(locations []Location) string {
	if len(locations) == 0 {
		return ""
	}

	files := make([]string, 0)
	for _, loc := range locations {
		files = append(files, filepath.Base(loc.File))
	}

	return fmt.Sprintf("Extract to shared utility. Found in: %s", strings.Join(files, ", "))
}

// estimateSavings calculates how many lines could be saved
func estimateSavings(duplicates []DuplicateBlock) string {
	totalSaved := 0
	for _, dup := range duplicates {
		if len(dup.Locations) > 0 {
			// Calculate savings: (copies - 1) * lines per copy
			saved := (len(dup.Locations) - 1) * dup.StatementCount
			totalSaved += saved
		}
	}

	if totalSaved == 0 {
		return "No significant duplicates found"
	}

	return fmt.Sprintf("~%d lines could be consolidated into shared utilities", totalSaved)
}
