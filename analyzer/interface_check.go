package analyzer

import "go/ast"

// checkTypeImplementsInterface checks if a type implements an interface
func (ia *InterfaceAnalyzer) checkTypeImplementsInterface(typeInfo *typeInfo, interfaceInfo *InterfaceInfo) *Implementation {
	impl := &Implementation{
		TypeName:           typeInfo.Name,
		Package:            typeInfo.Package,
		File:               typeInfo.File,
		Line:               typeInfo.Line,
		ImplementedMethods: []string{},
		MissingMethods:     []string{},
		Methods:            []MethodMatch{},
		Confidence:         0.85,
	}

	// Check each interface method
	for _, ifMethod := range interfaceInfo.Methods {
		// Look for matching method on the type
		found := false

		// Check with value receiver
		for _, fileAST := range ia.symbolAnalyzer.fileASTs {
			for _, decl := range fileAST.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Name.Name != ifMethod.Name || fn.Recv == nil {
					continue
				}

				receiverType := ia.symbolAnalyzer.typeExprToString(fn.Recv.List[0].Type)

				// Match with value or pointer receiver
				if receiverType == typeInfo.Name || receiverType == "*"+typeInfo.Name {
					impl.ImplementedMethods = append(impl.ImplementedMethods, ifMethod.Name)
					impl.Methods = append(impl.Methods, MethodMatch{
						Name:      fn.Name.Name,
						Receiver:  receiverType,
						Line:      ia.symbolAnalyzer.fset.Position(fn.Pos()).Line,
						Signature: ia.extractMethodSignature(fn),
					})
					found = true
					break
				}
			}

			if found {
				break
			}
		}

		if !found {
			impl.MissingMethods = append(impl.MissingMethods, ifMethod.Name)
		}
	}

	return impl
}
