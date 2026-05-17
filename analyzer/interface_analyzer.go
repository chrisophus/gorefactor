package analyzer

import (
	"fmt"
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

// typeInfo represents a struct type definition
type typeInfo struct {
	Name    string
	Package string
	File    string
	Line    int
	Fields  []string
}
