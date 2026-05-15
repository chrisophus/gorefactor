package orchestrator

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

// CodeInserter handles inserting new code snippets into existing Go files
type CodeInserter struct{}

// NewCodeInserter creates a new code inserter instance
func NewCodeInserter() *CodeInserter {
	return &CodeInserter{}
}

// InsertionLocation defines where to insert new code
type InsertionLocation struct {
	Type         string `json:"type"` // "before_function", "after_function", "inside_function", "at_end", "at_beginning"
	FunctionName string `json:"functionName,omitempty"`
	MethodName   string `json:"methodName,omitempty"`
	ReceiverType string `json:"receiverType,omitempty"`
	LineNumber   int    `json:"lineNumber,omitempty"`
	CodePattern  string `json:"codePattern,omitempty"`
}

// InsertionResult represents the result of a code insertion
type InsertionResult struct {
	File         string `json:"file"`
	Location     string `json:"location"`
	StartLine    int    `json:"startLine"`
	EndLine      int    `json:"endLine"`
	Description  string `json:"description"`
	InsertedCode string `json:"insertedCode"`
}

// InsertCode inserts a code snippet into a Go file at the specified location
func (ci *CodeInserter) InsertCode(filePath string, location *InsertionLocation, codeSnippet string) (*InsertionResult, error) {
	fset := token.NewFileSet()

	// Check if file exists
	fileExists := true
	_, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		fileExists = false
	}

	var node *ast.File

	if fileExists {
		// Parse existing file
		node, err = parser.ParseFile(fset, filePath, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("failed to parse file: %w", err)
		}
	} else {
		// For at_beginning on new files, we can just write the snippet directly
		if location.Type == "at_beginning" {
			// Write the file directly with the code snippet
			if err := os.WriteFile(filePath, []byte(codeSnippet), 0644); err != nil {
				return nil, fmt.Errorf("failed to create file: %w", err)
			}

			lines := strings.Split(codeSnippet, "\n")
			return &InsertionResult{
				File:         filePath,
				Location:     "at beginning of file (new file)",
				StartLine:    1,
				EndLine:      len(lines),
				Description:  "Created new file with code",
				InsertedCode: codeSnippet,
			}, nil
		}

		// For other locations, parse the code snippet as a file to create the AST
		// Check if snippet already has a package declaration
		trimmed := strings.TrimSpace(codeSnippet)
		var codeToParse string
		if strings.HasPrefix(trimmed, "package ") {
			// Already has package declaration, use as-is
			codeToParse = codeSnippet
		} else {
			// Wrap the snippet in a package declaration for parsing
			codeToParse = fmt.Sprintf("package main\n\n%s", codeSnippet)
		}

		// Parse with file path so it gets added to the file set
		node, err = parser.ParseFile(fset, filePath, []byte(codeToParse), parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("failed to parse code snippet: %w", err)
		}
	}

	var result *InsertionResult

	switch location.Type {
	case "before_function":
		result, err = ci.insertBeforeFunction(filePath, node, fset, location, codeSnippet)
	case "after_function":
		result, err = ci.insertAfterFunction(filePath, node, fset, location, codeSnippet)
	case "inside_function":
		result, err = ci.insertInsideFunction(filePath, node, fset, location, codeSnippet)
	case "at_end":
		result, err = ci.insertAtEnd(filePath, node, fset, codeSnippet)
	case "at_beginning":
		result, err = ci.insertAtBeginning(filePath, node, fset, codeSnippet)
	case "after_statement":
		result, err = ci.insertAfterStatement(filePath, node, fset, location, codeSnippet)
	default:
		return nil, fmt.Errorf("unknown insertion location type: %s", location.Type)
	}

	if err != nil {
		return nil, err
	}

	// Format and write the modified file
	if err := ci.writeFormattedFile(filePath, node, fset); err != nil {
		return nil, err
	}

	return result, nil
}

// insertBeforeFunction inserts code before a specific function
func (ci *CodeInserter) insertBeforeFunction(filePath string, node *ast.File, fset *token.FileSet, location *InsertionLocation, codeSnippet string) (*InsertionResult, error) {
	// Parse the code snippet
	snippetNode, err := ci.parseCodeSnippet(codeSnippet, fset)
	if err != nil {
		return nil, err
	}

	// Find the target function
	targetFunc := ci.findFunction(node, location.FunctionName, location.MethodName, location.ReceiverType)
	if targetFunc == nil {
		return nil, fmt.Errorf("target function not found")
	}

	// Find the position before the function
	insertPos := targetFunc.Pos()

	// Insert the snippet before the function
	ci.insertDeclarationsBefore(node, insertPos, snippetNode)

	startLine := fset.Position(insertPos).Line
	endLine := startLine + len(strings.Split(codeSnippet, "\n")) - 1

	return &InsertionResult{
		File:         filePath,
		Location:     fmt.Sprintf("before function %s", targetFunc.Name.Name),
		StartLine:    startLine,
		EndLine:      endLine,
		Description:  fmt.Sprintf("Inserted code before function '%s'", targetFunc.Name.Name),
		InsertedCode: codeSnippet,
	}, nil
}

// insertAfterFunction inserts code after a specific function
func (ci *CodeInserter) insertAfterFunction(filePath string, node *ast.File, fset *token.FileSet, location *InsertionLocation, codeSnippet string) (*InsertionResult, error) {
	// Parse the code snippet
	snippetNode, err := ci.parseCodeSnippet(codeSnippet, fset)
	if err != nil {
		return nil, err
	}

	// Find the target function
	targetFunc := ci.findFunction(node, location.FunctionName, location.MethodName, location.ReceiverType)
	if targetFunc == nil {
		return nil, fmt.Errorf("target function not found")
	}

	// Find the position after the function
	insertPos := targetFunc.End()

	// Insert the snippet after the function
	ci.insertDeclarationsAfter(node, insertPos, snippetNode)

	startLine := fset.Position(insertPos).Line
	endLine := startLine + len(strings.Split(codeSnippet, "\n")) - 1

	return &InsertionResult{
		File:         filePath,
		Location:     fmt.Sprintf("after function %s", targetFunc.Name.Name),
		StartLine:    startLine,
		EndLine:      endLine,
		Description:  fmt.Sprintf("Inserted code after function '%s'", targetFunc.Name.Name),
		InsertedCode: codeSnippet,
	}, nil
}

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

// findFunction finds a function by name and optional receiver type
func (ci *CodeInserter) findFunction(node *ast.File, functionName, methodName, receiverType string) *ast.FuncDecl {
	var targetFunc *ast.FuncDecl

	ast.Inspect(node, func(n ast.Node) bool {
		if funcDecl, ok := n.(*ast.FuncDecl); ok {
			if functionName != "" && funcDecl.Name.Name == functionName {
				targetFunc = funcDecl
				return false
			}
			if methodName != "" && funcDecl.Name.Name == methodName {
				if receiverType == "" {
					targetFunc = funcDecl
					return false
				}
				// Check receiver type
				if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
					if t, ok := funcDecl.Recv.List[0].Type.(*ast.StarExpr); ok {
						if ident, ok := t.X.(*ast.Ident); ok && ident.Name == receiverType {
							targetFunc = funcDecl
							return false
						}
					} else if ident, ok := funcDecl.Recv.List[0].Type.(*ast.Ident); ok && ident.Name == receiverType {
						targetFunc = funcDecl
						return false
					}
				}
			}
		}
		return true
	})

	return targetFunc
}

// insertDeclarationsBefore inserts declarations before a specific position
func (ci *CodeInserter) insertDeclarationsBefore(node *ast.File, pos token.Pos, declarations []ast.Decl) {
	// Find the index where to insert
	insertIndex := 0
	for i, decl := range node.Decls {
		if decl.Pos() >= pos {
			insertIndex = i
			break
		}
		insertIndex = i + 1
	}

	// Insert the declarations
	node.Decls = append(node.Decls[:insertIndex], append(declarations, node.Decls[insertIndex:]...)...)
}

// insertDeclarationsAfter inserts declarations after a specific position
func (ci *CodeInserter) insertDeclarationsAfter(node *ast.File, pos token.Pos, declarations []ast.Decl) {
	// Find the index where to insert
	insertIndex := len(node.Decls)
	for i, decl := range node.Decls {
		if decl.End() > pos {
			insertIndex = i + 1
			break
		}
	}

	// Insert the declarations
	node.Decls = append(node.Decls[:insertIndex], append(declarations, node.Decls[insertIndex:]...)...)
}

// insertDeclarationsAtEnd inserts declarations at the end of the file
func (ci *CodeInserter) insertDeclarationsAtEnd(node *ast.File, declarations []ast.Decl) {
	node.Decls = append(node.Decls, declarations...)
}

// insertDeclarationsAtBeginning inserts declarations at the beginning of the file
func (ci *CodeInserter) insertDeclarationsAtBeginning(node *ast.File, declarations []ast.Decl) {
	node.Decls = append(declarations, node.Decls...)
}

func (ci *CodeInserter) RemoveCodeBlock(filePath string,
	location *InsertionLocation, codePattern string) (*InsertionResult, error) {
	src, err := os.ReadFile(filePath)
	if err !=
		nil {
		return nil, fmt.
			Errorf("failed to read file: %w", err)
	}
	fset := token.
		NewFileSet()
	node, err := parser.ParseFile(fset, filePath, src,

		parser.ParseComments)
	if err != nil {
		return nil, fmt.
			Errorf("failed to parse file: %w",
				err)
	}
	targetFunc := ci.findFunction(node, location.FunctionName, location.MethodName, location.ReceiverType)
	if targetFunc == nil {
		name := location.FunctionName
		if name == "" {
			name = location.MethodName
		}
		return nil, fmt.Errorf("function not found: %s",
			name)
	}
	if targetFunc.Body == nil {
		return nil,
			fmt.Errorf("function %s has no body", targetFunc.Name.
				Name)
	}
	normPattern := stripWhitespace(codePattern)
	removedIdx := -1
	for i, stmt := range targetFunc.Body.List {
		start := fset.Position(stmt.Pos()).Offset
		end := fset.Position(stmt.End()).Offset
		if start < 0 || end > len(src) || end <=
			start {
			continue
		}
		if strings.Contains(stripWhitespace(string(src[start:end])), normPattern) {
			removedIdx = i
			break
		}
	}
	if removedIdx < 0 {
		return nil, fmt.Errorf("no statement matching %q found in %s", codePattern, targetFunc.Name.Name)
	}
	startLine := fset.Position(targetFunc.
		Body.List[removedIdx].
		Pos()).Line
	endLine :=
		fset.Position(targetFunc.
			Body.List[removedIdx].End()).Line
	targetFunc.Body.
		List = append(targetFunc.
		Body.List[:removedIdx], targetFunc.Body.
		List[removedIdx+1:]...,
	)
	if err := ci.writeFormattedFile(filePath, node, fset); err != nil {
		return nil, err
	}
	return &InsertionResult{File: filePath, Location: fmt.Sprintf("inside function %s", targetFunc.Name.Name), StartLine: startLine,
		EndLine: endLine, Description: fmt.Sprintf("Removed code block matching %q from %s",
			codePattern, targetFunc.Name.Name)}, nil
}
func stripWhitespace(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.
		ReplaceAll(s, "\t", "")
	s = strings.ReplaceAll(s,
		"\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}
func (ci *CodeInserter) writeFormattedFile(filePath string, node *ast.File, fset *token.FileSet) error {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, node); err != nil {
		return fmt.Errorf("failed to format code: %w", err)
	}
	normalized, err := format.Source(buf.Bytes())
	if err != nil {
		normalized = buf.Bytes()
	}
	if err := os.WriteFile(filePath, normalized, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}
func (ci *CodeInserter) parseCodeSnippet(codeSnippet string, fset *token.
	FileSet) ([]ast.
	Decl, error) {
	trimmed := strings.TrimSpace(codeSnippet)
	var wrappedCode string
	if strings.
		HasPrefix(trimmed, "package ") {
		wrappedCode = codeSnippet
	} else {
		wrappedCode = fmt.Sprintf("package main\n\n%s",
			codeSnippet)
	}
	node,

		err := parser.ParseFile(fset, "snippet", []byte(wrappedCode), parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse code snippet: %w", err)
	}
	return node.Decls, nil
}
func (ci *CodeInserter) parseCodeSnippetAsStatements(codeSnippet string, fset *token.
	FileSet) ([]ast.
	Stmt, error) {
	wrappedCode := fmt.Sprintf("package main\n\nfunc temp() {\n%s\n}",
		codeSnippet)
	node,

		err := parser.ParseFile(fset, "snippet", []byte(wrappedCode), parser.ParseComments)
	if err !=
		nil {
		return nil, fmt.
			Errorf("failed to parse code snippet: %w", err)
	}
	var stmts []ast.Stmt
	ast.Inspect(node,
		func(n ast.Node) bool {
			if funcDecl, ok := n.(*ast.FuncDecl); ok {
				if funcDecl.Body != nil {
					stmts = funcDecl.
						Body.List
				}
				return false
			}
			return true
		})
	return stmts, nil
}
func (ci *CodeInserter) ReplaceCodeBlock(filePath string, location *InsertionLocation, codePattern string, replacementCode string) (*InsertionResult, error) {
	src, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}
	targetFunc := ci.findFunction(node, location.FunctionName, location.MethodName, location.ReceiverType)
	if targetFunc == nil {
		name := location.FunctionName
		if name == "" {
			name = location.MethodName
		}
		return nil, fmt.Errorf("function not found: %s", name)
	}
	if targetFunc.Body == nil {
		return nil, fmt.Errorf("function %s has no body", targetFunc.Name.Name)
	}
	normPattern := stripWhitespace(codePattern)
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
		return nil, fmt.Errorf("no statement matching %q found in %s", codePattern, targetFunc.Name.Name)
	}
	replacementStmts, err := ci.parseCodeSnippetAsStatements(replacementCode, fset)
	if err != nil {
		return nil, fmt.Errorf("failed to parse replacement code: %w", err)
	}
	startLine := fset.Position(targetFunc.Body.List[foundIdx].Pos()).Line
	endLine := fset.Position(targetFunc.Body.List[foundIdx].End()).Line
	targetFunc.Body.List = append(
		targetFunc.Body.List[:foundIdx],
		append(replacementStmts, targetFunc.Body.List[foundIdx+1:]...)...,
	)
	if err := ci.writeFormattedFile(filePath, node, fset); err != nil {
		return nil, err
	}
	return &InsertionResult{
		File:         filePath,
		Location:     fmt.Sprintf("inside function %s", targetFunc.Name.Name),
		StartLine:    startLine,
		EndLine:      endLine,
		Description:  fmt.Sprintf("Replaced code matching %q in %s", codePattern, targetFunc.Name.Name),
		InsertedCode: replacementCode,
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
