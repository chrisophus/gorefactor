package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// ExtractionConfig holds parameters for tuning extraction recommendations
type ExtractionConfig struct {
	// Minimum complexity required for extraction (number of control structures)
	MinComplexity int `json:"minComplexity"`
	// Maximum complexity allowed for extraction
	MaxComplexity int `json:"maxComplexity"`
	// Maximum number of read variables allowed
	MaxReadVars int `json:"maxReadVars"`
	// Maximum number of write variables allowed
	MaxWriteVars int `json:"maxWriteVars"`
	// Minimum number of statements required
	MinStatements int `json:"minStatements"`
	// Maximum number of statements allowed
	MaxStatements int `json:"maxStatements"`
	// Number of leading statements to include before a block (0 = none)
	NumLeadingStmts int `json:"numLeadingStmts"`
}

// DefaultConfig returns the default configuration for extraction recommendations
func DefaultConfig() *ExtractionConfig {
	return &ExtractionConfig{
		MinComplexity:   1,
		MaxComplexity:   10,
		MaxReadVars:     20,
		MaxWriteVars:    10,
		MinStatements:   3,
		MaxStatements:   50,
		NumLeadingStmts: 1,
	}
}

// BlockInfo represents information about a code block
type BlockInfo struct {
	StartLine      int      `json:"startLine"`
	EndLine        int      `json:"endLine"`
	Variables      []string `json:"variables"`
	Assignments    []string `json:"assignments"`
	ReadVars       []string `json:"readVars"`
	WriteVars      []string `json:"writeVars"`
	Complexity     int      `json:"complexity"`
	IsExtractable  bool     `json:"isExtractable"`
	StatementCount int      `json:"statementCount"`

	// New metrics
	ControlStructures  int              `json:"controlStructures"`  // Number of if, for, switch, etc.
	LogicalOperators   int              `json:"logicalOperators"`   // Number of &&, || operators
	MaxNestingDepth    int              `json:"maxNestingDepth"`    // Maximum nesting level
	ErrorHandlingPaths int              `json:"errorHandlingPaths"` // Number of error handling paths
	ReturnCount        int              `json:"returnCount"`        // Number of return statements
	FunctionCalls      []string         `json:"functionCalls"`      // List of function calls
	VariableScopes     map[string][]int `json:"variableScopes"`     // Line ranges where variables are used
}

// RecommendExtractions analyzes a file and returns recommendations for method extraction.
// If functionName is non-empty, only the specified function is analyzed; otherwise, all functions are analyzed.

// Helper to recursively analyze all blocks

// Avoid duplicates by checking for overlapping ranges

// Look for function declarations

// AnalyzeBlock analyzes a code block and returns information about it

// Find all nodes in the given line range

// Analyze all nodes in the range

// Track variable assignments

// Track variable reads

// Track variables and their usage

// Track variable reads

// Track variable scope

// Track variable assignments

// Check for error handling pattern

// Update nesting depth

func AnalyzeBlock(filePath string, startLine, endLine int, config *ExtractionConfig) (*BlockInfo, error) {
	if config == nil {
		config = DefaultConfig()
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}
	info := &BlockInfo{StartLine: startLine, EndLine: endLine}
	var nodesInRange []ast.Node
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		pos := fset.Position(n.Pos())
		end := fset.Position(n.End())
		if pos.Line >= startLine && end.Line <= endLine {
			nodesInRange = append(nodesInRange, n)
		}
		return true
	})
	if len(nodesInRange) == 0 {
		return nil, nil
	}
	for _, n := range nodesInRange {
		applyRangeNode(n, info)
	}
	info.IsExtractable = isBlockExtractable(info, config)
	return info, nil
}
func analyzeBlock(block *ast.BlockStmt, info *BlockInfo) {
	readVars := make(map[string]bool)
	writeVars := make(map[string]bool)
	assignments := make(map[string]bool)
	variableScopes := make(map[string][]int)
	currentNesting := 0
	maxNesting := 0
	ast.Inspect(block, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.Ident:
			recordIdent(node, readVars, variableScopes)
		case *ast.AssignStmt:
			recordAssignment(node, info, writeVars, assignments)
		case *ast.IfStmt:
			recordIfStmt(node, info, &currentNesting, &maxNesting)
		case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt:
			recordLoopStmt(info, &currentNesting, &maxNesting)
		case *ast.BinaryExpr:
			applyBinaryExpr(node, info)
		case *ast.CallExpr:
			applyCallExpr(node, info)
		case *ast.ReturnStmt:
			info.ReturnCount++
			info.StatementCount++
		case *ast.ExprStmt, *ast.DeclStmt:
			info.StatementCount++
		}
		return true
	})
	info.MaxNestingDepth = maxNesting
	buildVariableInfo(readVars, writeVars, assignments, info)
	info.VariableScopes = variableScopes
}
func collectRecommendations(parent ast.Node, filePath string, fset *token.FileSet, config *ExtractionConfig, recommendations *[]*BlockInfo) {
	ast.Inspect(parent, func(n ast.Node) bool {
		block, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		startLine := fset.Position(block.Pos()).Line
		endLine := fset.Position(block.End()).Line
		info, err := AnalyzeBlock(filePath, startLine, endLine, config)
		if err != nil || info == nil || !info.IsExtractable {
			return true
		}
		for _, rec := range *recommendations {
			if rec.StartLine == info.StartLine && rec.EndLine == info.EndLine {
				return true
			}
		}
		*recommendations = append(*recommendations, info)
		return true
	})
}
func RecommendExtractions(filePath string, functionName string, config *ExtractionConfig) ([]*BlockInfo, error) {
	if config == nil {
		config = DefaultConfig()
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}
	var recommendations []*BlockInfo
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if funcDecl, ok := n.(*ast.FuncDecl); ok {
			if (functionName == "" || funcDecl.Name.Name == functionName) && funcDecl.Body != nil {
				collectRecommendations(funcDecl.Body, filePath, fset, config, &recommendations)
			}
		}
		return true
	})
	return recommendations, nil
}
