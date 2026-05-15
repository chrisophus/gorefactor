package analyzer

import (
	"fmt"
	"go/ast"
	"strings"
)

// Implementation represents a type that implements an interface
type Implementation struct {
	TypeName           string        `json:"typeName"`
	Package            string        `json:"package"`
	Methods            []MethodMatch `json:"methods"`
	ImplementedMethods []string      `json:"implementedMethods"`
	MissingMethods     []string      `json:"missingMethods"`
	ExplicitImpl       bool          `json:"explicitImpl"`
	Confidence         float64       `json:"confidence"`
	File               string        `json:"file"`
	Line               int           `json:"line"`
}

// MethodMatch represents a method that matches an interface method
type MethodMatch struct {
	Name      string `json:"name"`
	Receiver  string `json:"receiver"`
	Line      int    `json:"line"`
	Signature string `json:"signature"`
}

// InterfaceInfo represents an interface definition
type InterfaceInfo struct {
	Name    string        `json:"name"`
	Package string        `json:"package"`
	File    string        `json:"file"`
	Line    int           `json:"line"`
	Methods []MethodMatch `json:"methods"`
}

// ImplementationAnalysis contains all information about implementations
type ImplementationAnalysis struct {
	Interface            InterfaceInfo    `json:"interface"`
	Implementations      []Implementation `json:"implementations"`
	PartialImplements    []Implementation `json:"partialImplements"`
	DeadImplementations  []Implementation `json:"deadImplementations"`
	TotalImplementations int              `json:"totalImplementations"`
	Confidence           float64          `json:"confidence"`
}

// InterfaceAnalyzer analyzes interface implementations
type InterfaceAnalyzer struct {
	symbolAnalyzer  *UseAnalyzer
	files           []string
	interfaces      map[string]*InterfaceInfo
	implementations map[string][]Implementation
}

// NewInterfaceAnalyzer creates a new interface analyzer
func NewInterfaceAnalyzer(files []string) *InterfaceAnalyzer {
	return &InterfaceAnalyzer{
		symbolAnalyzer:  NewUseAnalyzer(files),
		files:           files,
		interfaces:      make(map[string]*InterfaceInfo),
		implementations: make(map[string][]Implementation),
	}
}

// FindImplementations finds all types that implement an interface
func (ia *InterfaceAnalyzer) FindImplementations(interfaceName string) (*ImplementationAnalysis, error) {
	// Parse all files
	if err := ia.symbolAnalyzer.Parse(); err != nil {
		return nil, err
	}

	ia.symbolAnalyzer.collectDefinitions()

	// Find the interface definition
	interfaceDef := ia.findInterfaceDefinition(interfaceName)
	if interfaceDef == nil {
		return nil, fmt.Errorf("interface not found: %s", interfaceName)
	}

	// Collect all types in the codebase
	allTypes := ia.collectAllTypes()

	// Check each type to see if it implements the interface
	var implementations []Implementation
	var partialImplements []Implementation

	for _, typeInfo := range allTypes {
		impl := ia.checkTypeImplementsInterface(typeInfo, interfaceDef)
		if impl != nil && len(impl.ImplementedMethods) > 0 {
			// Only include if type has at least one implemented method
			if len(impl.MissingMethods) == 0 {
				implementations = append(implementations, *impl)
			} else {
				partialImplements = append(partialImplements, *impl)
			}
		}
	}

	analysis := &ImplementationAnalysis{
		Interface:            *interfaceDef,
		Implementations:      implementations,
		PartialImplements:    partialImplements,
		TotalImplementations: len(implementations),
		Confidence:           0.9,
	}

	return analysis, nil
}

// VerifyInterfaceImpl checks if a specific type implements an interface
func (ia *InterfaceAnalyzer) VerifyInterfaceImpl(typeName, interfaceName string) (bool, []string, error) {
	if err := ia.symbolAnalyzer.Parse(); err != nil {
		return false, nil, err
	}

	ia.symbolAnalyzer.collectDefinitions()

	// Find interface and type
	interfaceDef := ia.findInterfaceDefinition(interfaceName)
	if interfaceDef == nil {
		return false, nil, fmt.Errorf("interface not found: %s", interfaceName)
	}

	typeInfo := ia.findTypeDefinition(typeName)
	if typeInfo == nil {
		return false, nil, fmt.Errorf("type not found: %s", typeName)
	}

	impl := ia.checkTypeImplementsInterface(typeInfo, interfaceDef)
	if impl == nil {
		return false, nil, nil
	}

	implements := len(impl.MissingMethods) == 0
	return implements, impl.MissingMethods, nil
}

// FindInterfaceUsers finds all places where an interface is used
func (ia *InterfaceAnalyzer) FindInterfaceUsers(interfaceName string) ([]SymbolUse, error) {
	if err := ia.symbolAnalyzer.Parse(); err != nil {
		return nil, err
	}

	query := SymbolQuery{Name: interfaceName, Type: TypeInterface}
	uses, err := ia.symbolAnalyzer.FindAllUses(query)
	if err != nil {
		return nil, err
	}

	return uses, nil
}

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

// typeInfo represents a struct type definition
type typeInfo struct {
	Name    string
	Package string
	File    string
	Line    int
	Fields  []string
}

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

	// Parameters
	if fn.Type.Params != nil {
		for i, param := range fn.Type.Params.List {
			if i > 0 {
				sig.WriteString(", ")
			}
			sig.WriteString(ia.symbolAnalyzer.typeExprToString(param.Type))
		}
	}

	sig.WriteString(")")

	// Return types
	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		sig.WriteString(" ")
		if len(fn.Type.Results.List) > 1 {
			sig.WriteString("(")
		}

		for i, result := range fn.Type.Results.List {
			if i > 0 {
				sig.WriteString(", ")
			}
			sig.WriteString(ia.symbolAnalyzer.typeExprToString(result.Type))
		}

		if len(fn.Type.Results.List) > 1 {
			sig.WriteString(")")
		}
	}

	return sig.String()
}

// extractFuncTypeSignature extracts signature from a func type
func (ia *InterfaceAnalyzer) extractFuncTypeSignature(fn *ast.FuncType) string {
	var sig strings.Builder

	sig.WriteString("(")

	if fn.Params != nil {
		for i, param := range fn.Params.List {
			if i > 0 {
				sig.WriteString(", ")
			}
			sig.WriteString(ia.symbolAnalyzer.typeExprToString(param.Type))
		}
	}

	sig.WriteString(")")

	if fn.Results != nil && len(fn.Results.List) > 0 {
		sig.WriteString(" ")
		if len(fn.Results.List) > 1 {
			sig.WriteString("(")
		}

		for i, result := range fn.Results.List {
			if i > 0 {
				sig.WriteString(", ")
			}
			sig.WriteString(ia.symbolAnalyzer.typeExprToString(result.Type))
		}

		if len(fn.Results.List) > 1 {
			sig.WriteString(")")
		}
	}

	return sig.String()
}
