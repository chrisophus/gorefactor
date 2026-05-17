package orchestrator

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"strings"
)

// insertInsideFunction inserts code inside a specific function
func (ci *CodeInserter) insertInsideFunction(filePath string, node *ast.File, fset *token.FileSet, location *InsertionLocation, codeSnippet string) (*InsertionResult, error) {
	// Parse the code snippet as statements
	snippetStmts, err := ci.parseCodeSnippetAsStatements(codeSnippet, fset)
	if err != nil {
		return nil, err
	}

	// Find the target function
	targetFunc := ci.findFunction(node, location.FunctionName, location.MethodName, location.ReceiverType)
	if targetFunc == nil {
		return nil, fmt.Errorf("target function not found")
	}

	// Insert statements at the beginning of the function body
	if targetFunc.Body == nil {
		targetFunc.Body = &ast.BlockStmt{}
	}

	// Insert statements at the beginning of the function
	targetFunc.Body.List = append(snippetStmts, targetFunc.Body.List...)

	startLine := fset.Position(targetFunc.Body.Pos()).Line
	endLine := startLine + len(strings.Split(codeSnippet, "\n")) - 1

	return &InsertionResult{
		File:         filePath,
		Location:     fmt.Sprintf("inside function %s", targetFunc.Name.Name),
		StartLine:    startLine,
		EndLine:      endLine,
		Description:  fmt.Sprintf("Inserted code inside function '%s'", targetFunc.Name.Name),
		InsertedCode: codeSnippet,
	}, nil
}

// insertAtEnd inserts code at the end of the file
func (ci *CodeInserter) insertAtEnd(filePath string, node *ast.File, fset *token.FileSet, codeSnippet string) (*InsertionResult, error) {
	// Parse the code snippet
	snippetNode, err := ci.parseCodeSnippet(codeSnippet, fset)
	if err != nil {
		return nil, err
	}

	// Insert at the end of the file
	ci.insertDeclarationsAtEnd(node, snippetNode)

	startLine := fset.Position(node.End()).Line
	endLine := startLine + len(strings.Split(codeSnippet, "\n")) - 1

	return &InsertionResult{
		File:         filePath,
		Location:     "at end of file",
		StartLine:    startLine,
		EndLine:      endLine,
		Description:  "Inserted code at end of file",
		InsertedCode: codeSnippet,
	}, nil
}

// insertAtBeginning inserts code at the beginning of the file
func (ci *CodeInserter) insertAtBeginning(filePath string, node *ast.File, fset *token.FileSet, codeSnippet string) (*InsertionResult, error) {
	// Parse the code snippet
	snippetNode, err := ci.parseCodeSnippet(codeSnippet, fset)
	if err != nil {
		return nil, err
	}

	// Insert at the beginning of the file (after package declaration)
	ci.insertDeclarationsAtBeginning(node, snippetNode)

	startLine := fset.Position(node.Package).Line + 1
	endLine := startLine + len(strings.Split(codeSnippet, "\n")) - 1

	return &InsertionResult{
		File:         filePath,
		Location:     "at beginning of file",
		StartLine:    startLine,
		EndLine:      endLine,
		Description:  "Inserted code at beginning of file",
		InsertedCode: codeSnippet,
	}, nil
}

func (ci *CodeInserter) insertAfterStatement(filePath string, node *ast.File, fset *token.FileSet, location *InsertionLocation, codeSnippet string) (*InsertionResult, error) {
	src, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	targetFunc := ci.findFunction(node, location.FunctionName, location.MethodName, location.ReceiverType)
	if targetFunc == nil {
		return nil, fmt.Errorf("target function not found")
	}
	if targetFunc.Body == nil {
		return nil, fmt.Errorf("function %s has no body", targetFunc.Name.Name)
	}
	normPattern := stripWhitespace(location.CodePattern)
	foundIdx := -1
	for i, stmt := range targetFunc.Body.List {
		start := fset.Position(stmt.Pos()).Offset
		end := fset.Position(stmt.End()).Offset
		if start < 0 || end > len(src) || end <= start {
			continue
		}
		if strings.Contains(stripWhitespace(string(src[start:end])), normPattern) {
			foundIdx = i
			break
		}
	}
	if foundIdx < 0 {
		return nil, fmt.Errorf("no statement matching %q found in function %s", location.CodePattern, targetFunc.Name.Name)
	}
	snippetStmts, err := ci.parseCodeSnippetAsStatements(codeSnippet, fset)
	if err != nil {
		return nil, err
	}
	targetFunc.Body.List = append(
		targetFunc.Body.List[:foundIdx+1],
		append(snippetStmts, targetFunc.Body.List[foundIdx+1:]...)...,
	)
	startLine := fset.Position(targetFunc.Body.List[foundIdx+1].Pos()).Line
	endLine := startLine + len(strings.Split(codeSnippet, "\n")) - 1
	return &InsertionResult{
		File:         filePath,
		Location:     fmt.Sprintf("after statement in function %s", targetFunc.Name.Name),
		StartLine:    startLine,
		EndLine:      endLine,
		Description:  fmt.Sprintf("Inserted code after statement matching %q in '%s'", location.CodePattern, targetFunc.Name.Name),
		InsertedCode: codeSnippet,
	}, nil
}
