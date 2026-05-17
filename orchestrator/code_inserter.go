package orchestrator

import (
	"fmt"
	"go/ast"
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

// Check if file exists

// Parse existing file

// For at_beginning on new files, we can just write the snippet directly

// Write the file directly with the code snippet

// For other locations, parse the code snippet as a file to create the AST
// Check if snippet already has a package declaration

// Already has package declaration, use as-is

// Wrap the snippet in a package declaration for parsing

// Parse with file path so it gets added to the file set

// Format and write the modified file

// insertBeforeFunction inserts code before a specific function

// Parse the code snippet

// Find the target function

// Find the position before the function

// Insert the snippet before the function

// insertAfterFunction inserts code after a specific function

// Parse the code snippet

// Find the target function

// Find the position after the function

func (ci *CodeInserter) insertNearFunction(filePath string, node *ast.File, fset *token.FileSet, location *InsertionLocation, codeSnippet string, before bool) (*InsertionResult, error) {
	snippetNode, err := ci.parseCodeSnippet(codeSnippet, fset)
	if err != nil {
		return nil, err
	}
	targetFunc := ci.findFunction(node, location.FunctionName, location.MethodName, location.ReceiverType)
	if targetFunc == nil {
		return nil, fmt.Errorf("target function not found")
	}
	var insertPos token.Pos
	var locStr, descFmt string
	if before {
		insertPos = targetFunc.Pos()
		ci.insertDeclarationsBefore(node, insertPos, snippetNode)
		locStr = fmt.Sprintf("before function %s", targetFunc.Name.Name)
		descFmt = "Inserted code before function '%s'"
	} else {
		insertPos = targetFunc.End()
		ci.insertDeclarationsAfter(node, insertPos, snippetNode)
		locStr = fmt.Sprintf("after function %s", targetFunc.Name.Name)
		descFmt = "Inserted code after function '%s'"
	}
	startLine := fset.Position(insertPos).Line
	endLine := startLine + len(strings.Split(codeSnippet, "\n")) - 1
	return &InsertionResult{
		File:         filePath,
		Location:     locStr,
		StartLine:    startLine,
		EndLine:      endLine,
		Description:  fmt.Sprintf(descFmt, targetFunc.Name.Name),
		InsertedCode: codeSnippet,
	}, nil
}
func (ci *CodeInserter) InsertCode(filePath string, location *InsertionLocation, codeSnippet string) (*InsertionResult, error) {
	fset := token.NewFileSet()
	node, immediate, err := ci.loadOrParseNode(filePath, location, codeSnippet, fset)
	if err != nil {
		return nil, err
	}
	if immediate != nil {
		return immediate, nil
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
	if err := ci.writeFormattedFile(filePath, node, fset); err != nil {
		return nil, err
	}
	return result, nil
}
// FindFunction is the exported version of findFunction for callers outside this package.
func (ci *CodeInserter) FindFunction(node *ast.File, functionName, methodName, receiverType string) *ast.FuncDecl {
	return ci.findFunction(node, functionName, methodName, receiverType)
}

func (ci *CodeInserter) findFunction(node *ast.File, functionName, methodName, receiverType string) *ast.FuncDecl {
	var targetFunc *ast.FuncDecl
	ast.Inspect(node, func(n ast.Node) bool {
		funcDecl, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		if functionName != "" && funcDecl.Name.Name == functionName {
			targetFunc = funcDecl
			return false
		}
		if methodName != "" && funcDecl.Name.Name == methodName {
			if receiverType == "" || matchesReceiverType(funcDecl, receiverType) {
				targetFunc = funcDecl
				return false
			}
		}
		return true
	})
	return targetFunc
}
func (ci *CodeInserter) RemoveCodeBlock(filePath string, location *InsertionLocation, codePattern string) (*InsertionResult, error) {
	src, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}
	targetFunc, err := ci.resolveTargetFunc(node, location)
	if err != nil {
		return nil, err
	}
	removedIdx := findStmtByPattern(src, targetFunc.Body.List, fset, stripWhitespace(codePattern))
	if removedIdx < 0 {
		return nil, fmt.Errorf("no statement matching %q found in %s", codePattern, targetFunc.Name.Name)
	}
	startLine := fset.Position(targetFunc.Body.List[removedIdx].Pos()).Line
	endLine := fset.Position(targetFunc.Body.List[removedIdx].End()).Line
	targetFunc.Body.List = append(targetFunc.Body.List[:removedIdx], targetFunc.Body.List[removedIdx+1:]...)
	if err := ci.writeFormattedFile(filePath, node, fset); err != nil {
		return nil, err
	}
	return &InsertionResult{
		File:        filePath,
		Location:    fmt.Sprintf("inside function %s", targetFunc.Name.Name),
		StartLine:   startLine,
		EndLine:     endLine,
		Description: fmt.Sprintf("Removed code block matching %q from %s", codePattern, targetFunc.Name.Name),
	}, nil
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
	targetFunc, err := ci.resolveTargetFunc(node, location)
	if err != nil {
		return nil, err
	}
	foundIdx := findStmtByPattern(src, targetFunc.Body.List, fset, stripWhitespace(codePattern))
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
