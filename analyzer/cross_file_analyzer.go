package analyzer

import (
	"crypto/md5"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// DuplicateBlock represents a code block that appears in multiple files
type DuplicateBlock struct {
	Pattern        string       `json:"pattern"`
	Hash           string       `json:"hash"`
	Locations      []Location   `json:"locations"`
	StatementCount int          `json:"statementCount"`
	Complexity     int          `json:"complexity"`
	ImpactScore    int          `json:"impactScore"`
	Recommendation string       `json:"recommendation"`
}

// Location represents where a duplicate block is found
type Location struct {
	File      string `json:"file"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
}

// CrossFileAnalysis holds all cross-file analysis results
type CrossFileAnalysis struct {
	DuplicateBlocks     []DuplicateBlock `json:"duplicateBlocks"`
	TotalFiles          int              `json:"totalFiles"`
	TotalFunctions      int              `json:"totalFunctions"`
	TotalMethods        int              `json:"totalMethods"`
	EstimatedSavings    string           `json:"estimatedSavings"`
	AnalysisTimestamp   string           `json:"analysisTimestamp"`
}

// FindDuplicateBlocks analyzes multiple files to find duplicate code blocks
// Returns deduplicated blocks found in multiple files, sorted by impact
// Note: Files that cannot be read or parsed are skipped silently
func FindDuplicateBlocks(files []string) ([]DuplicateBlock, error) {
	// Map of normalized code hash -> list of locations
	codeMap := make(map[string][]Location)
	// Map of hash -> original code block
	codeBlocks := make(map[string]string)
	// Map of hash -> block metadata
	blockMetadata := make(map[string]BlockMetadata)

	for _, filePath := range files {
		if !strings.HasSuffix(filePath, ".go") {
			continue
		}

		fileContent, err := os.ReadFile(filePath)
		if err != nil {
			// Skip files that cannot be read (e.g., permission denied)
			// This is acceptable as we're looking for patterns, not comprehensive coverage
			continue
		}

		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
		if err != nil {
			// Skip files that cannot be parsed (e.g., syntax errors)
			// This is acceptable as we're looking for patterns, not comprehensive coverage
			continue
		}

		// Extract all function/method bodies as potential duplicates
		ast.Inspect(node, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.FuncDecl:
				if d.Body != nil {
					extractBlocksFromFunc(d, filePath, fset, string(fileContent), codeMap, codeBlocks, blockMetadata)
				}
			}
			return true
		})
	}

	// Build list of duplicates (blocks appearing in 2+ files)
	var duplicates []DuplicateBlock
	seen := make(map[string]bool)

	for hash, locations := range codeMap {
		if len(locations) < 2 || seen[hash] {
			continue
		}
		seen[hash] = true

		block := DuplicateBlock{
			Hash:           hash,
			Pattern:        codeBlocks[hash],
			Locations:      locations,
			StatementCount: blockMetadata[hash].StatementCount,
			Complexity:     blockMetadata[hash].Complexity,
			ImpactScore:    calculateImpactScore(len(locations), blockMetadata[hash].StatementCount, blockMetadata[hash].Complexity),
			Recommendation: generateDuplicateRecommendation(locations),
		}
		duplicates = append(duplicates, block)
	}

	// Sort by impact score (descending)
	sort.Slice(duplicates, func(i, j int) bool {
		return duplicates[i].ImpactScore > duplicates[j].ImpactScore
	})

	return duplicates, nil
}

// BlockMetadata stores information about a code block
type BlockMetadata struct {
	StatementCount int
	Complexity     int
}

// extractBlocksFromFunc extracts code blocks from a function body
func extractBlocksFromFunc(fn *ast.FuncDecl, filePath string, fset *token.FileSet, fileContent string, codeMap map[string][]Location, codeBlocks map[string]string, blockMetadata map[string]BlockMetadata) {
	if fn.Body == nil {
		return
	}

	// Split file once to avoid O(n) splits for each block extraction
	fileLines := strings.Split(fileContent, "\n")

	startLine := fset.Position(fn.Body.Pos()).Line
	endLine := fset.Position(fn.Body.End()).Line

	// Extract the full function body
	code := extractCodeFromLinesSlice(fileLines, startLine, endLine)
	if code != "" {
		normalized := NormalizeCode(code)
		hash := hashCode(normalized)

		codeMap[hash] = append(codeMap[hash], Location{
			File:      filePath,
			StartLine: startLine,
			EndLine:   endLine,
		})
		codeBlocks[hash] = code
		blockMetadata[hash] = BlockMetadata{
			StatementCount: countStatements(fn.Body),
			Complexity:     countControlStructures(fn.Body),
		}
	}

	// Also extract individual blocks within the function
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch block := n.(type) {
		case *ast.BlockStmt:
			// Skip the function body itself (already extracted)
			if block == fn.Body {
				return true
			}

			bStartLine := fset.Position(block.Pos()).Line
			bEndLine := fset.Position(block.End()).Line

			// Only extract substantial blocks
			if bEndLine-bStartLine >= 2 {
				code := extractCodeFromLinesSlice(fileLines, bStartLine, bEndLine)
				if code != "" {
					normalized := NormalizeCode(code)
					hash := hashCode(normalized)

					codeMap[hash] = append(codeMap[hash], Location{
						File:      filePath,
						StartLine: bStartLine,
						EndLine:   bEndLine,
					})
					codeBlocks[hash] = code
					blockMetadata[hash] = BlockMetadata{
						StatementCount: countStatements(block),
						Complexity:     countControlStructures(block),
					}
				}
			}
		}
		return true
	})
}

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

// AnalyzeCrossFile performs a complete cross-file analysis on a directory
func AnalyzeCrossFile(dirPath string) (*CrossFileAnalysis, error) {
	files, err := findGoFiles(dirPath)
	if err != nil {
		return nil, err
	}

	duplicates, err := FindDuplicateBlocks(files)
	if err != nil {
		return nil, err
	}

	// Count functions/methods across all files
	totalFuncs := 0
	totalMethods := 0

	for _, filePath := range files {
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
		if err != nil {
			continue
		}

		for _, decl := range node.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				if fn.Recv != nil {
					totalMethods++
				} else {
					totalFuncs++
				}
			}
		}
	}

	analysis := &CrossFileAnalysis{
		DuplicateBlocks:   duplicates,
		TotalFiles:        len(files),
		TotalFunctions:    totalFuncs,
		TotalMethods:      totalMethods,
		EstimatedSavings:  estimateSavings(duplicates),
		AnalysisTimestamp: time.Now().Format(time.RFC3339),
	}

	return analysis, nil
}

// findGoFiles recursively finds all Go files in a directory
func findGoFiles(dirPath string) ([]string, error) {
	var files []string
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden directories and vendor
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") || info.Name() == "vendor" {
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
