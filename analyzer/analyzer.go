package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// BlockInfo represents information about a code block
type BlockInfo struct {
	StartLine     int      `json:"startLine"`
	EndLine       int      `json:"endLine"`
	Variables     []string `json:"variables"`
	Assignments   []string `json:"assignments"`
	ReadVars      []string `json:"readVars"`
	WriteVars     []string `json:"writeVars"`
	Complexity    int      `json:"complexity"`
	IsExtractable bool     `json:"isExtractable"`
}

// AnalyzeBlock analyzes a code block and returns information about it
func AnalyzeBlock(filePath string, startLine, endLine int) (*BlockInfo, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	info := &BlockInfo{
		StartLine: startLine,
		EndLine:   endLine,
	}

	// Find the smallest enclosing block for the given line range
	var (
		block        *ast.BlockStmt
		minBlockSize int = 1<<31 - 1 // max int
	)
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		b, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		pos := fset.Position(b.Pos())
		end := fset.Position(b.End())
		if pos.Line <= startLine && end.Line >= endLine {
			size := end.Line - pos.Line
			if size < minBlockSize {
				block = b
				minBlockSize = size
			}
		}
		return true
	})

	if block == nil {
		return nil, nil
	}

	// Analyze the block
	analyzeBlock(block, info)
	info.IsExtractable = isBlockExtractable(info)

	return info, nil
}

func analyzeBlock(block *ast.BlockStmt, info *BlockInfo) {
	// Track variables and their usage
	readVars := make(map[string]bool)
	writeVars := make(map[string]bool)
	assignments := make(map[string]bool)

	ast.Inspect(block, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.Ident:
			// Track variable reads
			if node.Obj != nil {
				readVars[node.Name] = true
			}
		case *ast.AssignStmt:
			// Track variable assignments
			for _, lhs := range node.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok {
					writeVars[ident.Name] = true
					assignments[ident.Name] = true
				}
			}
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt:
			// Increase complexity for control structures
			info.Complexity++
		}
		return true
	})

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
}

func isBlockExtractable(info *BlockInfo) bool {
	// A block is extractable if:
	// 1. It has some complexity (control structures)
	// 2. It doesn't have too many variables (to keep it manageable)
	// 3. All variables used are either assigned within the block or passed as parameters
	if info.Complexity == 0 {
		return false
	}
	if len(info.ReadVars) > 20 || len(info.WriteVars) > 10 {
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

// RecommendExtractions analyzes a file and returns recommendations for method extraction
func RecommendExtractions(filename string) ([]BlockInfo, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var recommendations []BlockInfo
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		ast.Inspect(funcDecl.Body, func(n ast.Node) bool {
			block, ok := n.(*ast.BlockStmt)
			if !ok {
				return true
			}

			info, err := AnalyzeBlock(filename, fset.Position(block.Pos()).Line, fset.Position(block.End()).Line)
			if err != nil {
				return true
			}
			if info.IsExtractable {
				recommendations = append(recommendations, *info)
			}
			return true
		})
	}
	return recommendations, nil
}
