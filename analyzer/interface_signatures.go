package analyzer

import (
	"go/ast"
	"strings"
)

// extractInterfaceMethods extracts methods from an interface type
func (ia *InterfaceAnalyzer) extractInterfaceMethods(interfaceType *ast.InterfaceType) []MethodMatch {
	var methods []MethodMatch

	if interfaceType.Methods == nil {
		return methods
	}

	for _, field := range interfaceType.Methods.List {
		if len(field.Names) == 0 {
			continue
		}

		for _, name := range field.Names {
			// Extract signature from the field
			sig := ""
			if fn, ok := field.Type.(*ast.FuncType); ok {
				sig = ia.extractFuncTypeSignature(fn)
			}

			methods = append(methods, MethodMatch{
				Name:      name.Name,
				Signature: sig,
				Line:      ia.symbolAnalyzer.fset.Position(name.Pos()).Line,
			})
		}
	}

	return methods
}

// extractStructFields extracts field names from a struct type
func (ia *InterfaceAnalyzer) extractStructFields(structType *ast.StructType) []string {
	var fields []string

	if structType.Fields == nil {
		return fields
	}

	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			fields = append(fields, name.Name)
		}
	}

	return fields
}

// extractMethodSignature extracts the signature of a function declaration
func (ia *InterfaceAnalyzer) extractMethodSignature(fn *ast.FuncDecl) string {
	var sig strings.Builder

	sig.WriteString(fn.Name.Name)
	sig.WriteString("(")
	if fn.Type.Params != nil {
		ia.writeTypeList(&sig, fn.Type.Params.List)
	}
	sig.WriteString(")")
	ia.writeResultTypes(&sig, fn.Type.Results)

	return sig.String()

}

// extractFuncTypeSignature extracts signature from a func type
func (ia *InterfaceAnalyzer) extractFuncTypeSignature(fn *ast.FuncType) string {
	var sig strings.Builder

	sig.WriteString("(")
	if fn.Params != nil {
		ia.writeTypeList(&sig, fn.Params.List)
	}
	sig.WriteString(")")
	ia.writeResultTypes(&sig, fn.Results)

	return sig.String()

}

func (ia *InterfaceAnalyzer) writeTypeList(sig *strings.Builder, fields []*ast.Field) {
	for i, field := range fields {
		if i > 0 {
			sig.WriteString(", ")
		}
		sig.WriteString(ia.symbolAnalyzer.typeExprToString(field.Type))
	}
}

func (ia *InterfaceAnalyzer) writeResultTypes(sig *strings.Builder, results *ast.FieldList) {
	if results == nil || len(results.List) == 0 {
		return
	}
	sig.WriteString(" ")
	if len(results.List) > 1 {
		sig.WriteString("(")
	}
	ia.writeTypeList(sig, results.List)
	if len(results.List) > 1 {
		sig.WriteString(")")
	}
}
