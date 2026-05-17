package analyzer

import "go/ast"

// findInterfaceDefinition finds an interface by name
func (ia *InterfaceAnalyzer) findInterfaceDefinition(name string) *InterfaceInfo {
	for file, fileAST := range ia.symbolAnalyzer.fileASTs {
		for _, decl := range fileAST.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}

			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || typeSpec.Name.Name != name {
					continue
				}

				interfaceType, ok := typeSpec.Type.(*ast.InterfaceType)
				if !ok {
					continue
				}

				// Extract interface methods
				interfaceInfo := &InterfaceInfo{
					Name:    name,
					Package: fileAST.Name.Name,
					File:    file,
					Line:    ia.symbolAnalyzer.fset.Position(typeSpec.Pos()).Line,
					Methods: ia.extractInterfaceMethods(interfaceType),
				}

				return interfaceInfo
			}
		}
	}

	return nil
}

// findTypeDefinition finds a type by name
func (ia *InterfaceAnalyzer) findTypeDefinition(typeName string) *typeInfo {
	for file, fileAST := range ia.symbolAnalyzer.fileASTs {
		for _, decl := range fileAST.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}

			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || typeSpec.Name.Name != typeName {
					continue
				}

				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}

				return &typeInfo{
					Name:    typeName,
					Package: fileAST.Name.Name,
					File:    file,
					Line:    ia.symbolAnalyzer.fset.Position(typeSpec.Pos()).Line,
					Fields:  ia.extractStructFields(structType),
				}
			}
		}
	}

	return nil
}

// collectAllTypes collects all struct types in the codebase
func (ia *InterfaceAnalyzer) collectAllTypes() []*typeInfo {
	var types []*typeInfo

	for fileName, fileAST := range ia.symbolAnalyzer.fileASTs {
		for _, decl := range fileAST.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}

			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}

				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}

				types = append(types, &typeInfo{
					Name:    typeSpec.Name.Name,
					Package: fileAST.Name.Name,
					File:    fileName,
					Line:    ia.symbolAnalyzer.fset.Position(typeSpec.Pos()).Line,
					Fields:  ia.extractStructFields(structType),
				})
			}
		}
	}

	return types
}
