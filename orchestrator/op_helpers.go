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

// executeCreateFile creates a new file with the specified content

func declLabel(decl ast.Decl, fset *token.FileSet) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return fmt.Sprintf("function '%s'", d.Name.Name)
	case *ast.GenDecl:
		return genDeclLabel(d)
	}
	return "declaration"
}

// Create code inserter and insert code

func genDeclLabel(d *ast.GenDecl) string {
	switch d.Tok {
	case token.TYPE:
		if len(d.Specs) > 0 {
			if ts, ok := d.Specs[0].(*ast.TypeSpec); ok {
				return fmt.Sprintf("type '%s'", ts.Name.Name)
			}
		}
		return "type declaration"
	case token.CONST:
		return "const declaration"
	case token.VAR:
		if len(d.Specs) > 0 {
			if vs, ok := d.Specs[0].(*ast.ValueSpec); ok && len(vs.Names) > 0 {
				return fmt.Sprintf("var '%s'", vs.Names[0].Name)
			}
		}
		return "var declaration"
	}
	return "generic declaration"
}

// Convert location data to InsertionLocation

func findDeclInRange(decls []ast.Decl, fset *token.FileSet, startLine, endLine int) (ast.Decl, int, string, error) {
	for i, decl := range decls {
		s := fset.Position(decl.Pos()).Line
		e := fset.Position(decl.End()).Line
		if s <= startLine && e >= endLine {
			return decl, i, declLabel(decl, fset), nil
		}
	}
	var info []string
	for i, decl := range decls {
		s := fset.Position(decl.Pos()).Line
		e := fset.Position(decl.End()).Line
		info = append(info, fmt.Sprintf("  %d: %s (lines %d-%d)", i, declLabel(decl, fset), s, e))
	}
	list := strings.Join(info, "\n")
	if list == "" {
		list = "  (no declarations found)"
	}
	return nil, -1, "", fmt.Errorf("available declarations:\n%s", list)
}

// Get parameters

func writeFileAndImport(path string, node *ast.File, fset *token.FileSet) error {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, node); err != nil {
		return fmt.Errorf("failed to format %s: %w", path, err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	if err := formatImports(path); err != nil {
		fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", path, err)
	}
	return nil
}

// executeInsertCode executes a code insertion operation

func recordMoveResults(result *OperationResult, sourceFile, destFile string, sourceStart, sourceEnd int, declCode, declType string) {
	destStartLine, destEndLine := 1, 1
	if content, err := os.ReadFile(destFile); err == nil {
		destFset := token.NewFileSet()
		if parsedNode, err := parser.ParseFile(destFset, destFile, content, parser.ParseComments); err == nil && len(parsedNode.Decls) > 0 {
			last := parsedNode.Decls[len(parsedNode.Decls)-1]
			destStartLine = destFset.Position(last.Pos()).Line
			destEndLine = destFset.Position(last.End()).Line
		}
	}
	result.Changes = append(result.Changes, &CodeChange{
		Type:        "move_method",
		File:        sourceFile,
		StartLine:   sourceStart,
		EndLine:     sourceEnd,
		Description: fmt.Sprintf("Moved %s to %s", declType, destFile),
		OldCode:     declCode,
	})
	result.Changes = append(result.Changes, &CodeChange{
		Type:        "move_method",
		File:        destFile,
		StartLine:   destStartLine,
		EndLine:     destEndLine,
		Description: fmt.Sprintf("Added %s from %s", declType, sourceFile),
		NewCode:     declCode,
	})
}

// Fallback if parsing fails - still record the change

func extractOldName(target *TargetSpecification) string {
	if target == nil {
		return ""
	}
	if target.FunctionName != "" {
		return target.FunctionName
	}
	return target.MethodName
}

// Record changes with detailed information

func renameInFile(filename string, fileNode *ast.File, fset *token.FileSet, oldName, newName string, result *OperationResult) error {
	changed := false
	ast.Inspect(fileNode, func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok && ident.Name == oldName {
			ident.Name = newName
			changed = true
		}
		return true
	})
	if !changed {
		return nil
	}
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, fileNode); err != nil {
		return fmt.Errorf("failed to format %s: %w", filename, err)
	}
	normalized, nErr := format.Source(buf.Bytes())
	if nErr != nil {
		normalized = buf.Bytes()
	}
	if err := os.WriteFile(filename, normalized, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", filename, err)
	}
	result.Changes = append(result.Changes, &CodeChange{
		Type:        "rename_declaration",
		File:        filename,
		Description: fmt.Sprintf("Renamed %q to %q in %s", oldName, newName, filename),
	})
	return nil
}

// Find the last declaration (the one we just added)

func appendDeclToFile(filePath, declCode, packageName string) error {
	var content []byte
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		content = []byte(fmt.Sprintf("package %s\n", packageName))
	} else {
		var err error
		content, err = os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read destination file: %w", err)
		}
	}
	content = append(bytes.TrimRight(content, "\n\r"), []byte("\n\n"+declCode+"\n")...)
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		return fmt.Errorf("failed to write destination file: %w", err)
	}
	if err := formatImports(filePath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", filePath, err)
	}
	return nil
}
