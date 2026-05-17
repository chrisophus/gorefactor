package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"gorefactor/util"
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
	codeMap := make(map[string][]Location)
	codeBlocks := make(map[string]string)
	blockMetadata := make(map[string]BlockMetadata)

	processDuplicateBlocksFromFiles(files, codeMap, codeBlocks, blockMetadata)
	duplicates := buildAndSortDuplicates(codeMap, codeBlocks, blockMetadata)

	return duplicates, nil
}

// BlockMetadata stores information about a code block
type BlockMetadata struct {
	StatementCount int
	Complexity     int
}

func processDuplicateBlocksFromFiles(files []string, codeMap map[string][]Location, codeBlocks map[string]string, blockMetadata map[string]BlockMetadata) {
	for _, filePath := range files {
		if !strings.HasSuffix(filePath, ".go") {
			continue
		}

		fileContent, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
		if err != nil {
			continue
		}

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
}

func buildAndSortDuplicates(codeMap map[string][]Location, codeBlocks map[string]string, blockMetadata map[string]BlockMetadata) []DuplicateBlock {
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

	sort.Slice(duplicates, func(i, j int) bool {
		return duplicates[i].ImpactScore > duplicates[j].ImpactScore
	})

	return duplicates
}

// extractBlocksFromFunc extracts code blocks from a function body
func extractBlocksFromFunc(fn *ast.FuncDecl, filePath string, fset *token.FileSet, fileContent string, codeMap map[string][]Location, codeBlocks map[string]string, blockMetadata map[string]BlockMetadata) {
	if fn.Body == nil {
		return
	}

	fileLines := strings.Split(fileContent, "\n")
	extractFullFunctionBody(fn, filePath, fset, fileLines, codeMap, codeBlocks, blockMetadata)
	extractIndividualBlocks(fn, filePath, fset, fileLines, codeMap, codeBlocks, blockMetadata)
}

func extractFullFunctionBody(fn *ast.FuncDecl, filePath string, fset *token.FileSet, fileLines []string, codeMap map[string][]Location, codeBlocks map[string]string, blockMetadata map[string]BlockMetadata) {
	startLine := fset.Position(fn.Body.Pos()).Line
	endLine := fset.Position(fn.Body.End()).Line

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
}

func extractIndividualBlocks(fn *ast.FuncDecl, filePath string, fset *token.FileSet, fileLines []string, codeMap map[string][]Location, codeBlocks map[string]string, blockMetadata map[string]BlockMetadata) {
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch block := n.(type) {
		case *ast.BlockStmt:
			if block == fn.Body {
				return true
			}

			bStartLine := fset.Position(block.Pos()).Line
			bEndLine := fset.Position(block.End()).Line

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

// AnalyzeCrossFile performs a complete cross-file analysis on a directory
func AnalyzeCrossFile(dirPath string) (*CrossFileAnalysis, error) {
	files, err := util.FindGoFiles(dirPath)
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
