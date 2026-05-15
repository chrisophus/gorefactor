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
func (o *Orchestrator) executeMoveMethod(operation *RefactoringOperation, target *TargetLocation, result *OperationResult) error {
	newFile, ok := operation.Parameters["newFile"].(string)
	if !ok {
		return fmt.Errorf("newFile parameter is required for move_method operation")
	}

	fset := token.NewFileSet()

	// Parse source file
	sourceNode, err := parser.ParseFile(fset, target.File, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse source file: %w", err)
	}

	// Re-find the target using the same FileSet for accurate positions
	// This ensures line numbers match between finding and moving
	actualTarget, err := o.findTarget(operation.Target, target.File)
	if err != nil {
		return fmt.Errorf("failed to re-find target: %w", err)
	}

	// Find the declaration to move using line numbers from the same FileSet
	var declToMove ast.Decl
	declIndex := -1
	var declType string

	for i, decl := range sourceNode.Decls {
		startLine := fset.Position(decl.Pos()).Line
		endLine := fset.Position(decl.End()).Line

		// Check if this declaration matches the target
		// Declaration should start at or before target start and end at or after target end
		if startLine <= actualTarget.StartLine && endLine >= actualTarget.EndLine {
			declToMove = decl
			declIndex = i

			// Determine declaration type for better error messages and logging
			switch d := decl.(type) {
			case *ast.FuncDecl:
				declType = fmt.Sprintf("function '%s'", d.Name.Name)
			case *ast.GenDecl:
				switch d.Tok {
				case token.TYPE:
					if len(d.Specs) > 0 {
						if ts, ok := d.Specs[0].(*ast.TypeSpec); ok {
							declType = fmt.Sprintf("type '%s'", ts.Name.Name)
						} else {
							declType = "type declaration"
						}
					} else {
						declType = "type declaration"
					}
				case token.CONST:
					declType = "const declaration"
				case token.VAR:
					if len(d.Specs) > 0 {
						if vs, ok := d.Specs[0].(*ast.ValueSpec); ok && len(vs.Names) > 0 {
							declType = fmt.Sprintf("var '%s'", vs.Names[0].Name)
						} else {
							declType = "var declaration"
						}
					} else {
						declType = "var declaration"
					}
				default:
					declType = "generic declaration"
				}
			default:
				declType = "declaration"
			}
			break
		}
	}

	if declToMove == nil {
		// Provide helpful error message with available declarations
		var declInfo []string
		for i, decl := range sourceNode.Decls {
			startLine := fset.Position(decl.Pos()).Line
			endLine := fset.Position(decl.End()).Line
			switch d := decl.(type) {
			case *ast.FuncDecl:
				declInfo = append(declInfo, fmt.Sprintf("  %d: function '%s' (lines %d-%d)", i, d.Name.Name, startLine, endLine))
			case *ast.GenDecl:
				switch d.Tok {
				case token.TYPE:
					if len(d.Specs) > 0 {
						if ts, ok := d.Specs[0].(*ast.TypeSpec); ok {
							declInfo = append(declInfo, fmt.Sprintf("  %d: type '%s' (lines %d-%d)", i, ts.Name.Name, startLine, endLine))
						}
					}
				case token.CONST:
					declInfo = append(declInfo, fmt.Sprintf("  %d: const block (lines %d-%d)", i, startLine, endLine))
				case token.VAR:
					declInfo = append(declInfo, fmt.Sprintf("  %d: var block (lines %d-%d)", i, startLine, endLine))
				}
			}
		}
		declList := strings.Join(declInfo, "\n")
		if declList == "" {
			declList = "  (no declarations found)"
		}
		return fmt.Errorf("declaration not found at lines %d-%d in file %s\nAvailable declarations:\n%s", actualTarget.StartLine, actualTarget.EndLine, target.File, declList)
	}

	// Extract the code snippet for the declaration
	var declBuf bytes.Buffer
	if err := format.Node(&declBuf, fset, declToMove); err != nil {
		return fmt.Errorf("failed to format declaration: %w", err)
	}
	declCode := declBuf.String()

	// Collect comments associated with this declaration
	declStart := declToMove.Pos()
	declEnd := declToMove.End()
	var commentsToMove []*ast.CommentGroup
	var newSourceComments []*ast.CommentGroup

	for _, commentGroup := range sourceNode.Comments {
		if commentBelongsToDecl(fset, declStart, declEnd, commentGroup) {
			commentsToMove = append(commentsToMove, commentGroup)
		} else {
			newSourceComments = append(newSourceComments, commentGroup)
		}
	}

	// Remove declaration from source file
	sourceNode.Decls = append(sourceNode.Decls[:declIndex], sourceNode.Decls[declIndex+1:]...)
	// Update source file comments
	sourceNode.Comments = newSourceComments

	// Write modified source file
	var sourceBuf bytes.Buffer
	if err := format.Node(&sourceBuf, fset, sourceNode); err != nil {
		return fmt.Errorf("failed to format source file: %w", err)
	}
	if err := os.WriteFile(target.File, sourceBuf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write source file: %w", err)
	}

	// Run goimports on source file to fix imports
	cmd := exec.Command("goimports", "-w", target.File)
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: goimports on %s: %v\n", target.File, err)
	}

	// Parse or create destination file
	var destNode *ast.File
	destExists := true
	if _, err := os.Stat(newFile); os.IsNotExist(err) {
		destExists = false
	}

	if destExists {
		destNode, err = parser.ParseFile(fset, newFile, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("failed to parse destination file: %w", err)
		}
	} else {
		// Create new file with package declaration
		// Try to extract package name from source file
		packageName := sourceNode.Name.Name
		destNode = &ast.File{
			Name:     ast.NewIdent(packageName),
			Decls:    []ast.Decl{},
			Comments: []*ast.CommentGroup{},
		}
	}

	// Add declaration to destination file (at the end)
	destNode.Decls = append(destNode.Decls, declToMove)
	// Add comments to destination file
	destNode.Comments = append(destNode.Comments, commentsToMove...)

	// Write destination file
	var destBuf bytes.Buffer
	if err := format.Node(&destBuf, fset, destNode); err != nil {
		return fmt.Errorf("failed to format destination file: %w", err)
	}
	destContent := destBuf.Bytes()
	if err := os.WriteFile(newFile, destContent, 0644); err != nil {
		return fmt.Errorf("failed to write destination file: %w", err)
	}

	// Run goimports on destination file to fix imports
	cmd = exec.Command("goimports", "-w", newFile)
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: goimports on %s: %v\n", newFile, err)
	}

	// Re-read the file after goimports may have modified it
	updatedDestContent, err := os.ReadFile(newFile)
	if err != nil {
		updatedDestContent = destContent // Fallback to original content
	}

	// Parse the written file to get accurate line numbers for the added declaration
	destFset := token.NewFileSet()
	parsedDestNode, err := parser.ParseFile(destFset, newFile, updatedDestContent, parser.ParseComments)
	if err == nil && len(parsedDestNode.Decls) > 0 {
		// Find the last declaration (the one we just added)
		lastDecl := parsedDestNode.Decls[len(parsedDestNode.Decls)-1]
		destStartLine := destFset.Position(lastDecl.Pos()).Line
		destEndLine := destFset.Position(lastDecl.End()).Line

		// Record changes with detailed information
		result.Changes = append(result.Changes, &CodeChange{
			Type:        "move_method",
			File:        target.File,
			StartLine:   actualTarget.StartLine,
			EndLine:     actualTarget.EndLine,
			Description: fmt.Sprintf("Moved %s to %s", declType, newFile),
			OldCode:     declCode,
			NewCode:     "",
		})

		result.Changes = append(result.Changes, &CodeChange{
			Type:        "move_method",
			File:        newFile,
			StartLine:   destStartLine,
			EndLine:     destEndLine,
			Description: fmt.Sprintf("Added %s from %s", declType, target.File),
			NewCode:     declCode,
		})
	} else {
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

		result.Changes = append(result.Changes, &CodeChange{
			Type:        "move_method",
			File:        target.File,
			StartLine:   actualTarget.StartLine,
			EndLine:     actualTarget.EndLine,
			Description: fmt.Sprintf("Moved %s to %s", declType, newFile),
			OldCode:     declCode,
			NewCode:     "",
		})

		result.Changes = append(result.Changes, &CodeChange{
			Type:        "move_method",
			File:        newFile,
			StartLine:   1,
			EndLine:     1,
			Description: fmt.Sprintf("Added %s from %s", declType, target.File),
			NewCode:     declCode,
		})
	}

	return nil
}

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
func (o *Orchestrator) executeRenameDeclaration(operation *RefactoringOperation, result *OperationResult) error {
	oldName := ""
	if operation.Target != nil {
		oldName = operation.Target.FunctionName
		if oldName == "" {
			oldName = operation.Target.MethodName
		}
	}
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
			changed := false
			ast.Inspect(fileNode, func(n ast.Node) bool {
				if ident, ok := n.(*ast.Ident); ok && ident.Name == oldName {
					ident.Name = newName
					changed = true
				}
				return true
			})
			if !changed {
				continue
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
		}
	}
	if len(result.Changes) == 0 {
		return fmt.Errorf("identifier %q not found in package", oldName)
	}
	return nil
}
