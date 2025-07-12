package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
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
func RecommendExtractions(filePath string, functionName string, config *ExtractionConfig) ([]*BlockInfo, error) {
	if config == nil {
		config = DefaultConfig()
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var recommendations []*BlockInfo

	// Helper to recursively analyze all blocks
	recursiveAnalyze := func(parent ast.Node, fset *token.FileSet, config *ExtractionConfig) {
		ast.Inspect(parent, func(n ast.Node) bool {
			block, ok := n.(*ast.BlockStmt)
			if ok {
				startLine := fset.Position(block.Pos()).Line
				endLine := fset.Position(block.End()).Line
				info, err := AnalyzeBlock(filePath, startLine, endLine, config)
				if err == nil && info != nil && info.IsExtractable {
					// Avoid duplicates by checking for overlapping ranges
					duplicate := false
					for _, rec := range recommendations {
						if rec.StartLine == info.StartLine && rec.EndLine == info.EndLine {
							duplicate = true
							break
						}
					}
					if !duplicate {
						recommendations = append(recommendations, info)
					}
				}
			}
			return true
		})
	}

	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		// Look for function declarations
		if funcDecl, ok := n.(*ast.FuncDecl); ok {
			if functionName == "" || funcDecl.Name.Name == functionName {
				if funcDecl.Body != nil {
					recursiveAnalyze(funcDecl.Body, fset, config)
				}
			}
		}
		return true
	})

	return recommendations, nil
}

// AnalyzeBlock analyzes a code block and returns information about it
func AnalyzeBlock(filePath string, startLine, endLine int, config *ExtractionConfig) (*BlockInfo, error) {
	if config == nil {
		config = DefaultConfig()
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	info := &BlockInfo{
		StartLine: startLine,
		EndLine:   endLine,
	}

	// Find all nodes in the given line range
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

	// Analyze all nodes in the range
	for _, n := range nodesInRange {
		switch node := n.(type) {
		case *ast.BlockStmt:
			analyzeBlock(node, info, nil)
		case *ast.AssignStmt:
			// Track variable assignments
			for _, lhs := range node.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok {
					info.WriteVars = append(info.WriteVars, ident.Name)
					info.Assignments = append(info.Assignments, ident.Name)
				}
			}
			info.StatementCount++
		case *ast.Ident:
			// Track variable reads
			if node.Obj != nil {
				info.ReadVars = append(info.ReadVars, node.Name)
			}
		case *ast.IfStmt:
			info.ControlStructures++
			info.Complexity++
			info.StatementCount++
		case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt:
			info.ControlStructures++
			info.Complexity++
			info.StatementCount++
		case *ast.BinaryExpr:
			if node.Op == token.LAND || node.Op == token.LOR {
				info.LogicalOperators++
				info.Complexity++
			}
		case *ast.CallExpr:
			if ident, ok := node.Fun.(*ast.Ident); ok {
				info.FunctionCalls = append(info.FunctionCalls, ident.Name)
			}
			info.StatementCount++
		case *ast.ReturnStmt:
			info.ReturnCount++
			info.StatementCount++
		case *ast.ExprStmt:
			info.StatementCount++
		case *ast.DeclStmt:
			info.StatementCount++
		}
	}

	info.IsExtractable = isBlockExtractable(info, config)
	return info, nil
}

func analyzeBlock(block *ast.BlockStmt, info *BlockInfo, t *testing.T) {
	// Track variables and their usage
	readVars := make(map[string]bool)
	writeVars := make(map[string]bool)
	assignments := make(map[string]bool)
	variableScopes := make(map[string][]int)
	currentNesting := 0
	maxNesting := 0

	ast.Inspect(block, func(n ast.Node) bool {
		if t != nil {
			t.Logf("Visiting node type: %T", n)
		}
		switch node := n.(type) {
		case *ast.Ident:
			// Track variable reads
			if node.Obj != nil {
				readVars[node.Name] = true
				// Track variable scope
				if _, exists := variableScopes[node.Name]; !exists {
					variableScopes[node.Name] = []int{int(node.Pos())}
				}
				variableScopes[node.Name] = append(variableScopes[node.Name], int(node.End()))
			}
		case *ast.AssignStmt:
			// Track variable assignments
			for _, lhs := range node.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok {
					writeVars[ident.Name] = true
					assignments[ident.Name] = true
				}
			}
			info.StatementCount++
		case *ast.IfStmt:
			info.ControlStructures++
			info.Complexity++
			currentNesting++
			if currentNesting > maxNesting {
				maxNesting = currentNesting
			}
			// Check for error handling pattern
			if len(node.Body.List) > 0 {
				if _, ok := node.Body.List[0].(*ast.ReturnStmt); ok {
					info.ErrorHandlingPaths++
				}
			}
			info.StatementCount++
		case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt:
			info.ControlStructures++
			info.Complexity++
			currentNesting++
			if currentNesting > maxNesting {
				maxNesting = currentNesting
			}
			info.StatementCount++
		case *ast.BinaryExpr:
			if node.Op == token.LAND || node.Op == token.LOR {
				info.LogicalOperators++
				info.Complexity++
			}
		case *ast.CallExpr:
			if ident, ok := node.Fun.(*ast.Ident); ok {
				info.FunctionCalls = append(info.FunctionCalls, ident.Name)
			}
			info.StatementCount++
		case *ast.ReturnStmt:
			info.ReturnCount++
			info.StatementCount++
		case *ast.ExprStmt:
			info.StatementCount++
		case *ast.DeclStmt:
			info.StatementCount++
		}
		return true
	})

	// Update nesting depth
	info.MaxNestingDepth = maxNesting

	// Convert maps to slices
	for v := range readVars {
		info.ReadVars = append(info.ReadVars, v)
	}
	for v := range writeVars {
		info.WriteVars = append(info.WriteVars, v)
	}
	for v := range assignments {
		info.Assignments = append(info.Assignments, v)
	}
	info.VariableScopes = variableScopes
}

func isBlockExtractable(info *BlockInfo, config *ExtractionConfig) bool {
	if config == nil {
		config = DefaultConfig()
	}

	// Check complexity bounds
	if info.Complexity < config.MinComplexity || info.Complexity > config.MaxComplexity {
		return false
	}

	// Check variable counts
	if len(info.ReadVars) > config.MaxReadVars || len(info.WriteVars) > config.MaxWriteVars {
		return false
	}

	// Check statement count
	if info.StatementCount < config.MinStatements || info.StatementCount > config.MaxStatements {
		return false
	}

	// Check if all read variables are either written to in the block
	// or should be passed as parameters
	for _, readVar := range info.ReadVars {
		isWritten := false
		for _, writeVar := range info.WriteVars {
			if readVar == writeVar {
				isWritten = true
				break
			}
		}
		if !isWritten {
			// This variable is read but not written to in the block
			// It needs to be passed as a parameter
			info.Variables = append(info.Variables, readVar)
		}
	}

	return true
}
