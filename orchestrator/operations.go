package orchestrator

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// commentBelongsToDecl returns true if a comment group should be associated with a declaration.
// If the comment lies inside the declaration's tokens, or if it ends within one blank line above the declaration.
func commentBelongsToDecl(fileSet *token.FileSet, declStart, declEnd token.Pos, commentGroups *ast.CommentGroup) bool {
	// Inside the declaration: always include.
	if commentGroups.Pos() >= declStart && commentGroups.End() <= declEnd {
		return true
	}
	// Otherwise, if the comment lies above the declaration and its end is within one blank line.
	declLine := fileSet.Position(declStart).Line
	commentGroupsEndLine := fileSet.Position(commentGroups.End()).Line
	if declLine > commentGroupsEndLine && (declLine-commentGroupsEndLine) <= 2 {
		return true
	}
	return false
}

// executeMoveMethod executes a method moving operation

// Parse source file

// Re-find the target using the same FileSet for accurate positions
// This ensures line numbers match between finding and moving

// Find the declaration to move using line numbers from the same FileSet

// Check if this declaration matches the target
// Declaration should start at or before target start and end at or after target end

// Determine declaration type for better error messages and logging

// Provide helpful error message with available declarations

// Extract the code snippet for the declaration

// Collect comments associated with this declaration

// Remove declaration from source file

// Update source file comments

// Write modified source file

// Run goimports on source file to fix imports

// Parse or create destination file

// Create new file with package declaration
// Try to extract package name from source file

// Add declaration to destination file (at the end)

// Add comments to destination file

// Write destination file

// Run goimports on destination file to fix imports

// Re-read the file after goimports may have modified it

// Fallback to original content

// Parse the written file to get accurate line numbers for the added declaration

// Find the last declaration (the one we just added)

// Record changes with detailed information

// Fallback if parsing fails - still record the change

// executeInsertCode executes a code insertion operation

// Get parameters

// Convert location data to InsertionLocation

// Create code inserter and insert code

// executeCreateFile creates a new file with the specified content

// Check if file already exists

// File exists - check if we should skip or overwrite

// No changes applied

// Write the file

func (o *Orchestrator) executeInsertCode(operation *RefactoringOperation, result *OperationResult) error {

	codeSnippet, ok := operation.Parameters["codeSnippet"].(string)
	if !ok {
		return fmt.Errorf("codeSnippet parameter is required for insert_code operation")
	}

	locationData, ok := operation.Parameters["location"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("location parameter is required for insert_code operation")
	}

	location := &InsertionLocation{
		Type: locationData["type"].(string),
	}
	if functionName, ok := locationData["functionName"].(string); ok {
		location.FunctionName = functionName
	}
	if methodName, ok := locationData["methodName"].(string); ok {
		location.MethodName = methodName
	}
	if receiverType, ok := locationData["receiverType"].(string); ok {
		location.ReceiverType = receiverType
	}
	if lineNumber, ok := locationData["lineNumber"].(float64); ok {
		location.LineNumber = int(lineNumber)
	}
	if codePattern, ok := locationData["codePattern"].(string); ok {
		location.CodePattern = codePattern
	}

	inserter := NewCodeInserter()
	insertionResult, err := inserter.InsertCode(operation.File, location, codeSnippet)
	if err != nil {
		return fmt.Errorf("failed to insert code: %w", err)
	}

	result.Changes = append(result.Changes, &CodeChange{
		Type:        "insert_code",
		File:        insertionResult.File,
		StartLine:   insertionResult.StartLine,
		EndLine:     insertionResult.EndLine,
		Description: insertionResult.Description,
		NewCode:     insertionResult.InsertedCode,
	})

	return nil
}

func (o *Orchestrator) executeCreateFile(operation *RefactoringOperation, result *OperationResult) error {
	codeSnippet, ok := operation.Parameters["codeSnippet"].(string)
	if !ok {
		return fmt.Errorf("codeSnippet parameter is required for create_file operation")
	}

	if _, err := os.Stat(operation.File); err == nil {

		if operation.Fallback != nil && operation.Fallback.Type == "skip" {
			result.Success = true
			result.Applied = false
			result.Message = "File already exists and fallback is skip"
			result.Changes = []*CodeChange{}
			return nil
		}
	}

	if err := os.WriteFile(operation.File, []byte(codeSnippet), 0644); err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	lines := strings.Split(codeSnippet, "\n")
	result.Changes = append(result.Changes, &CodeChange{
		Type:        "create_file",
		File:        operation.File,
		StartLine:   1,
		EndLine:     len(lines),
		Description: "Created new file",
		NewCode:     codeSnippet,
	})

	return nil
}
func (o *Orchestrator) executeReplaceCode(operation *RefactoringOperation, result *OperationResult) error {
	codePattern, _ := operation.Parameters["codePattern"].(string)
	if codePattern == "" {
		return fmt.Errorf("codePattern parameter is required for replace_code")
	}
	replacementCode, _ := operation.Parameters["replacementCode"].(string)
	if replacementCode == "" {
		return fmt.Errorf("replacementCode parameter is required for replace_code")
	}
	locationMap, ok := operation.Parameters["location"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("location parameter is required for replace_code")
	}
	funcName, _ := locationMap["functionName"].(string)
	methodName, _ := locationMap["methodName"].(string)
	receiverType, _ := locationMap["receiverType"].(string)
	ci := NewCodeInserter()
	ins, err := ci.ReplaceCodeBlock(operation.File, &InsertionLocation{
		FunctionName: funcName,
		MethodName:   methodName,
		ReceiverType: receiverType,
	}, codePattern, replacementCode)
	if err != nil {
		return err
	}
	result.Changes = append(result.Changes, &CodeChange{
		Type:        "replace_code",
		File:        operation.File,
		StartLine:   ins.StartLine,
		EndLine:     ins.EndLine,
		Description: ins.Description,
		NewCode:     ins.InsertedCode,
	})
	return nil
}
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
	if err := exec.Command("goimports", "-w", operation.File).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: goimports on %s: %v\n", operation.File, err)
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

func declLabel(decl ast.Decl, fset *token.FileSet) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return fmt.Sprintf("function '%s'", d.Name.Name)
	case *ast.GenDecl:
		return genDeclLabel(d)
	}
	return "declaration"
}
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
func writeFileAndImport(path string, node *ast.File, fset *token.FileSet) error {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, node); err != nil {
		return fmt.Errorf("failed to format %s: %w", path, err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	if err := exec.Command("goimports", "-w", path).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: goimports on %s: %v\n", path, err)
	}
	return nil
}

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

func extractOldName(target *TargetSpecification) string {
	if target == nil {
		return ""
	}
	if target.FunctionName != "" {
		return target.FunctionName
	}
	return target.MethodName
}
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
	if err := exec.Command("goimports", "-w", filePath).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: goimports on %s: %v\n", filePath, err)
	}
	return nil
}
func (o *Orchestrator) executeMoveMethod(operation *RefactoringOperation, target *TargetLocation, result *OperationResult) error {
	newFile, ok := operation.Parameters["newFile"].(string)
	if !ok {
		return fmt.Errorf("newFile parameter is required for move_method operation")
	}
	src, err := os.ReadFile(target.File)
	if err != nil {
		return fmt.Errorf("failed to read source file: %w", err)
	}
	fset := token.NewFileSet()
	sourceNode, err := parser.ParseFile(fset, target.File, src, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse source file: %w", err)
	}
	actualTarget, err := o.findTarget(operation.Target, target.File)
	if err != nil {
		return fmt.Errorf("failed to re-find target: %w", err)
	}
	declToMove, declIndex, declType, err := findDeclInRange(sourceNode.Decls, fset, actualTarget.StartLine, actualTarget.EndLine)
	if err != nil {
		return fmt.Errorf("declaration not found at lines %d-%d in file %s\n%s", actualTarget.StartLine, actualTarget.EndLine, target.File, err.Error())
	}
	textStart := fset.Position(declToMove.Pos()).Offset
	textEnd := fset.Position(declToMove.End()).Offset
	var newSourceComments []*ast.CommentGroup
	for _, cg := range sourceNode.Comments {
		if commentBelongsToDecl(fset, declToMove.Pos(), declToMove.End(), cg) {
			if off := fset.Position(cg.Pos()).Offset; off < textStart {
				textStart = off
			}
		} else {
			newSourceComments = append(newSourceComments, cg)
		}
	}
	declCode := string(bytes.TrimSpace(src[textStart:textEnd]))
	sourceNode.Decls = append(sourceNode.Decls[:declIndex], sourceNode.Decls[declIndex+1:]...)
	sourceNode.Comments = newSourceComments
	if err := writeFileAndImport(target.File, sourceNode, fset); err != nil {
		return err
	}
	if err := appendDeclToFile(newFile, declCode, sourceNode.Name.Name); err != nil {
		return err
	}
	recordMoveResults(result, target.File, newFile, actualTarget.StartLine, actualTarget.EndLine, declCode, declType)
	return nil
}
