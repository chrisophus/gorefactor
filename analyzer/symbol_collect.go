package analyzer

import (
	"go/ast"
	"strings"
)

// collectDefinitions walks all AST nodes and collects symbol definitions
func (ua *UseAnalyzer) collectDefinitions() {
	for filePath, node := range ua.fileASTs {
		ua.currentFile = filePath
		ua.currentPackage = node.Name.Name

		// Parse imports
		for _, imp := range node.Imports {
			alias := ""
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			path := strings.Trim(imp.Path.Value, "\"")
			if alias == "" {
				// Use last part of path as default
				parts := strings.Split(path, "/")
				alias = parts[len(parts)-1]
			}
			ua.imports[alias] = path
		}

		// Collect declarations
		for _, decl := range node.Decls {
			ua.collectDeclaration(decl, filePath)
		}
	}
}

// collectDeclaration collects a single declaration
func (ua *UseAnalyzer) collectDeclaration(decl ast.Decl, filePath string) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		def := &SymbolDefinition{
			Name:       d.Name.Name,
			Type:       TypeFunction,
			File:       filePath,
			Line:       ua.fset.Position(d.Pos()).Line,
			Column:     ua.fset.Position(d.Pos()).Column,
			Package:    ua.currentPackage,
			IsExported: ast.IsExported(d.Name.Name),
			Snippet:    d.Name.Name,
		}

		// If it has a receiver, it's a method
		if d.Recv != nil && len(d.Recv.List) > 0 {
			def.Type = TypeMethod
			def.Receiver = ua.typeExprToString(d.Recv.List[0].Type)
			key := ua.buildDefinitionKey(d.Name.Name, def.Receiver)
			ua.definitions[key] = def
		} else {
			ua.definitions[d.Name.Name] = def
		}

	case *ast.GenDecl:
		for _, spec := range d.Specs {
			ua.collectTypeSpec(spec, filePath)
		}
	}
}

// collectTypeSpec collects type, const, and var declarations
func (ua *UseAnalyzer) collectTypeSpec(spec ast.Spec, filePath string) {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		def := &SymbolDefinition{
			Name:       s.Name.Name,
			File:       filePath,
			Line:       ua.fset.Position(s.Pos()).Line,
			Column:     ua.fset.Position(s.Pos()).Column,
			Package:    ua.currentPackage,
			IsExported: ast.IsExported(s.Name.Name),
			Snippet:    s.Name.Name,
		}

		switch s.Type.(type) {
		case *ast.StructType:
			def.Type = TypeStruct
		case *ast.InterfaceType:
			def.Type = TypeInterface
		default:
			def.Type = TypeType
		}

		ua.definitions[s.Name.Name] = def

	case *ast.ValueSpec:
		for _, ident := range s.Names {
			def := &SymbolDefinition{
				Name:       ident.Name,
				Type:       TypeVariable,
				File:       filePath,
				Line:       ua.fset.Position(ident.Pos()).Line,
				Column:     ua.fset.Position(ident.Pos()).Column,
				Package:    ua.currentPackage,
				IsExported: ast.IsExported(ident.Name),
				Snippet:    ident.Name,
			}
			ua.definitions[ident.Name] = def
		}
	}
}

// collectUses walks the AST and collects all symbol uses
func (ua *UseAnalyzer) collectUses(query SymbolQuery) {
	for filePath, node := range ua.fileASTs {
		ua.currentFile = filePath
		ua.currentPackage = node.Name.Name

		// First, identify method receiver contexts
		ua.identifyMethodContexts(node)

		// Walk the AST using Inspect
		walker := &useWalker{
			ua:    ua,
			query: query,
		}
		ast.Inspect(node, walker.inspectNode)
	}
}

// identifyMethodContexts maps method bodies to their receiver types
func (ua *UseAnalyzer) identifyMethodContexts(node *ast.File) {
	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv != nil && len(fn.Recv.List) > 0 {
			receiverType := ua.typeExprToString(fn.Recv.List[0].Type)
			if fn.Body != nil {
				ua.receiverContext[fn.Body] = receiverType
			}
		}
	}
}
