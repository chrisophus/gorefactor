package orchestrator

import (
	"go/ast"
	"go/token"
)

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
		if decl.End() >= pos {
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
	// Go requires every import declaration to precede all other declarations,
	// so "beginning" means after the trailing import, not before it.
	i := 0
	for i < len(node.Decls) {
		gd, ok := node.Decls[i].(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			break
		}
		i++
	}
	rest := append([]ast.Decl(nil), node.Decls[i:]...)
	node.Decls = append(append(node.Decls[:i:i], declarations...), rest...)

}

func (ci *CodeInserter) insertBeforeFunction(filePath string, node *ast.File, fset *token.FileSet, location *InsertionLocation, codeSnippet string) (*InsertionResult, error) {
	return ci.insertNearFunction(filePath, node, fset, location, codeSnippet, true)
}

func (ci *CodeInserter) insertAfterFunction(filePath string, node *ast.File, fset *token.FileSet, location *InsertionLocation, codeSnippet string) (*InsertionResult, error) {
	return ci.insertNearFunction(filePath, node, fset, location, codeSnippet, false)
}
