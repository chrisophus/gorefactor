package orchestrator

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
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
