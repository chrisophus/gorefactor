package extractor

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
)

// ExtractionResult represents the result of a method extraction
type ExtractionResult struct {
	OriginalFile string   `json:"originalFile"`
	NewFile      string   `json:"newFile"`
	MethodName   string   `json:"methodName"`
	Parameters   []string `json:"parameters"`
	ReturnValues []string `json:"returnValues"`
}

// ExtractMethod extracts a code block into a new method
func ExtractMethod(filePath string, startLine, endLine int, methodName string) (*ExtractionResult, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	// Find the smallest enclosing block for the given line range
	var (
		block        *ast.BlockStmt
		parentFunc   *ast.FuncDecl
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
		return nil, fmt.Errorf("block not found")
	}

	// Find the parent function
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if fn, ok := n.(*ast.FuncDecl); ok {
			if fn.Body != nil && fn.Body.Pos() <= block.Pos() && fn.Body.End() >= block.End() {
				parentFunc = fn
				return false
			}
		}
		return true
	})

	if parentFunc == nil {
		return nil, fmt.Errorf("parent function not found")
	}

	// Analyze variables used in the block
	usedVars := analyzeBlockVariables(block, parentFunc)

	// Create the new method
	newMethod := createNewMethod(parentFunc, block, methodName, usedVars)

	// Insert the new method after the parent function
	insertMethodAfter(node, parentFunc, newMethod)

	// Replace the block with a method call
	replaceBlockWithCall(block, methodName, usedVars)

	// Format the modified file
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, node); err != nil {
		return nil, err
	}

	// Write the modified file
	if err := os.WriteFile(filePath, buf.Bytes(), 0644); err != nil {
		return nil, err
	}

	return &ExtractionResult{
		OriginalFile: filePath,
		NewFile:      filePath,
		MethodName:   methodName,
		Parameters:   usedVars,
	}, nil
}

type variableInfo struct {
	name      string
	isRead    bool
	isWritten bool
}

func analyzeBlockVariables(block *ast.BlockStmt, parentFunc *ast.FuncDecl) []string {
	// Track variables used in the block
	vars := make(map[string]*variableInfo)

	// First, collect all variables from the parent function
	for _, param := range parentFunc.Type.Params.List {
		for _, name := range param.Names {
			vars[name.Name] = &variableInfo{
				name:      name.Name,
				isRead:    false,
				isWritten: false,
			}
		}
	}

	// Then analyze the block
	ast.Inspect(block, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.Ident:
			if info, exists := vars[node.Name]; exists {
				info.isRead = true
			}
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok {
					if info, exists := vars[ident.Name]; exists {
						info.isWritten = true
					}
				}
			}
		}
		return true
	})

	// Collect variables that need to be passed as parameters
	var result []string
	for _, info := range vars {
		if info.isRead && !info.isWritten {
			result = append(result, info.name)
		}
	}

	return result
}

func createNewMethod(parentFunc *ast.FuncDecl, block *ast.BlockStmt, methodName string, usedVars []string) *ast.FuncDecl {
	// Create parameter list
	var params []*ast.Field
	for _, v := range usedVars {
		params = append(params, &ast.Field{
			Names: []*ast.Ident{ast.NewIdent(v)},
			Type:  ast.NewIdent("interface{}"), // TODO: Determine actual type
		})
	}

	// Create the new method
	newMethod := &ast.FuncDecl{
		Recv: parentFunc.Recv,
		Name: ast.NewIdent(methodName),
		Type: &ast.FuncType{
			Params: &ast.FieldList{
				List: params,
			},
			Results: parentFunc.Type.Results,
		},
		Body: block,
	}

	return newMethod
}

func insertMethodAfter(file *ast.File, after *ast.FuncDecl, newMethod *ast.FuncDecl) {
	for i, decl := range file.Decls {
		if decl == after {
			// Insert the new method after the parent function
			file.Decls = append(file.Decls[:i+1], append([]ast.Decl{newMethod}, file.Decls[i+1:]...)...)
			return
		}
	}
}

func replaceBlockWithCall(block *ast.BlockStmt, methodName string, usedVars []string) {
	// Create argument list
	var args []ast.Expr
	for _, v := range usedVars {
		args = append(args, ast.NewIdent(v))
	}

	// Create the method call
	call := &ast.CallExpr{
		Fun:  ast.NewIdent(methodName),
		Args: args,
	}

	// Replace the block with the call
	*block = ast.BlockStmt{
		List: []ast.Stmt{
			&ast.ExprStmt{X: call},
		},
	}
}
