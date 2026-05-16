package orchestrator

import (
	"bytes"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
)

// Check if file already exists

func (o *Orchestrator) executeDeleteDeclaration(operation *RefactoringOperation, target *TargetLocation, result *OperationResult) error {
	src, err := os.ReadFile(operation.File)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, operation.File, src, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse file: %w", err)
	}
	removeIdx := -1
	for i, decl := range node.Decls {
		start := fset.Position(decl.Pos()).Line
		end := fset.Position(decl.End()).Line
		if start <= target.StartLine && end >= target.EndLine {
			removeIdx = i
			break
		}
	}
	if removeIdx < 0 {
		return fmt.Errorf("declaration not found at lines %d-%d in %s", target.StartLine, target.EndLine, operation.File)
	}
	startLine := fset.Position(node.Decls[removeIdx].Pos()).Line
	endLine := fset.Position(node.Decls[removeIdx].End()).Line
	node.Decls = append(node.Decls[:removeIdx], node.Decls[removeIdx+1:]...)
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, node); err != nil {
		return fmt.Errorf("failed to format file: %w", err)
	}
	normalized, nErr := format.Source(buf.Bytes())
	if nErr != nil {
		normalized = buf.Bytes()
	}
	if err := os.WriteFile(operation.File, normalized, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	if err := formatImports(operation.File); err != nil {
		fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", operation.File, err)
	}
	result.Changes = append(result.Changes, &CodeChange{
		Type:        "delete_declaration",
		File:        operation.File,
		StartLine:   startLine,
		EndLine:     endLine,
		Description: fmt.Sprintf("Deleted declaration at lines %d-%d from %s", startLine, endLine, operation.File),
	})
	return nil
}

func (o *Orchestrator) executeRenameDeclaration(operation *RefactoringOperation, result *OperationResult) error {
	oldName := extractOldName(operation.Target)
	if oldName == "" {
		return fmt.Errorf("target functionName or methodName is required for rename_declaration")
	}
	newName, _ := operation.Parameters["newName"].(string)
	if newName == "" {
		return fmt.Errorf("newName parameter is required for rename_declaration")
	}
	if len(oldName) > 0 && oldName[0] >= 'A' && oldName[0] <= 'Z' {
		return fmt.Errorf("rename_declaration only supports unexported identifiers; use gopls rename for exported symbols")
	}
	dir := filepath.Dir(operation.File)
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse package directory: %w", err)
	}
	for _, pkg := range pkgs {
		for filename, fileNode := range pkg.Files {
			if err := renameInFile(filename, fileNode, fset, oldName, newName, result); err != nil {
				return err
			}
		}
	}
	if len(result.Changes) == 0 {
		return fmt.Errorf("identifier %q not found in package", oldName)
	}
	return nil
}
