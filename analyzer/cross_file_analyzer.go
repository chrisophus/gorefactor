package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"time"
)

// DuplicateBlock represents a code block that appears in multiple files
type DuplicateBlock struct {
	Pattern        string     `json:"pattern"`
	Hash           string     `json:"hash"`
	Locations      []Location `json:"locations"`
	StatementCount int        `json:"statementCount"`
	Complexity     int        `json:"complexity"`
	ImpactScore    int        `json:"impactScore"`
	Recommendation string     `json:"recommendation"`
}

// Location represents where a duplicate block is found
type Location struct {
	File      string `json:"file"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
}

// CrossFileAnalysis holds all cross-file analysis results
type CrossFileAnalysis struct {
	DuplicateBlocks   []DuplicateBlock `json:"duplicateBlocks"`
	TotalFiles        int              `json:"totalFiles"`
	TotalFunctions    int              `json:"totalFunctions"`
	TotalMethods      int              `json:"totalMethods"`
	EstimatedSavings  string           `json:"estimatedSavings"`
	AnalysisTimestamp string           `json:"analysisTimestamp"`
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

			// Only extract substantial blocks. Improvement plan item 6a: a
			// block must hold at least MinDuplicateStatements statements — one-
			// and two-statement blocks are almost always idiomatic (guard
			// clauses, error returns) and not worth consolidating.
			if bEndLine-bStartLine >= 2 && countStatements(block) >= MinDuplicateStatements {
				code := extractCodeFromLinesSlice(fileLines, bStartLine, bEndLine)
				// Item 6b: canonical error-handling idioms are excluded outright.
				if code != "" && !isIdiomaticErrorBlock(NormalizeCode(code)) {
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

// FindDuplicateBlocksInDir walks dirPath with walk options, then finds duplicate
// function-body blocks. Prefer this over AnalyzeCrossFile when you only need
// duplicates and want generated/vendor trees skipped (same as lint).
func FindDuplicateBlocksInDir(dirPath string, walk WalkOptions) ([]DuplicateBlock, error) {
	files, err := WalkGoFiles(dirPath, walk)
	if err != nil {
		return nil, fmt.Errorf("walk go files: %w", err)
	}
	return FindDuplicateBlocks(files)
}

// AnalyzeCrossFile performs a complete cross-file analysis on a directory.
// Generated *.gen.go / *_gen.go files and standard vendor/.git trees are skipped.
func AnalyzeCrossFile(dirPath string) (*CrossFileAnalysis, error) {
	return analyzeCrossFile(dirPath, DefaultWalkOptions())
}

func analyzeCrossFile(dirPath string, walk WalkOptions) (*CrossFileAnalysis, error) {
	files, err := WalkGoFiles(dirPath, walk)
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
