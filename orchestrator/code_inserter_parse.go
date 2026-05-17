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

func (ci *CodeInserter) loadOrParseNode(filePath string, location *InsertionLocation, codeSnippet string, fset *token.FileSet) (*ast.File, *InsertionResult, error) {
	_, err := os.Stat(filePath)
	if !os.IsNotExist(err) {
		node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse file: %w", err)
		}
		return node, nil, nil
	}
	if location.Type == "at_beginning" {
		if err := os.WriteFile(filePath, []byte(codeSnippet), 0644); err != nil {
			return nil, nil, fmt.Errorf("failed to create file: %w", err)
		}
		lines := strings.Split(codeSnippet, "\n")
		return nil, &InsertionResult{
			File:         filePath,
			Location:     "at beginning of file (new file)",
			StartLine:    1,
			EndLine:      len(lines),
			Description:  "Created new file with code",
			InsertedCode: codeSnippet,
		}, nil
	}
	trimmed := strings.TrimSpace(codeSnippet)
	var codeToParse string
	if strings.HasPrefix(trimmed, "package ") {
		codeToParse = codeSnippet
	} else {
		codeToParse = fmt.Sprintf("package main\n\n%s", codeSnippet)
	}
	node, err := parser.ParseFile(fset, filePath, []byte(codeToParse), parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse code snippet: %w", err)
	}
	return node, nil, nil
}

func (ci *CodeInserter) resolveTargetFunc(node *ast.File, location *InsertionLocation) (*ast.FuncDecl, error) {
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
	return targetFunc, nil
}

// Check receiver type

func stripWhitespace(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.
		ReplaceAll(s, "\t", "")
	s = strings.ReplaceAll(s,
		"\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

// findFunction finds a function by name and optional receiver type

func matchesReceiverType(funcDecl *ast.FuncDecl, receiverType string) bool {
	if funcDecl.Recv == nil || len(funcDecl.Recv.List) == 0 {
		return false
	}
	if t, ok := funcDecl.Recv.List[0].Type.(*ast.StarExpr); ok {
		if ident, ok := t.X.(*ast.Ident); ok && ident.Name == receiverType {
			return true
		}
	}
	if ident, ok := funcDecl.Recv.List[0].Type.(*ast.Ident); ok && ident.Name == receiverType {
		return true
	}
	return false
}

// Insert the snippet after the function

func findStmtByPattern(src []byte, stmts []ast.Stmt, fset *token.FileSet, normPattern string) int {
	for i, stmt := range stmts {
		start := fset.Position(stmt.Pos()).Offset
		end := fset.Position(stmt.End()).Offset
		if start < 0 || end > len(src) || end <= start {
			continue
		}
		if strings.Contains(stripWhitespace(string(src[start:end])), normPattern) {
			return i
		}
	}
	return -1
}
