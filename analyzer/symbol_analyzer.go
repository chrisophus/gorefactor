package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

// UseContext describes how a symbol is being used
type UseContext string

const (
	UsageCall      UseContext = "call"      // func() or method() call
	UsageRead      UseContext = "read"      // var used in expression
	UsageWrite     UseContext = "write"     // var = something (assignment target)
	UsageDefine    UseContext = "define"    // var := value (declaration)
	UsageParameter UseContext = "parameter" // in function parameters
	UsageReturn    UseContext = "return"    // in return statement
	UsageDefer     UseContext = "defer"     // defer statement
	UsagePass      UseContext = "pass"      // passed as function argument
	UsageType      UseContext = "type"      // used as a type
	UsageReceiver  UseContext = "receiver"  // method receiver
)

// SymbolType represents the kind of symbol
type SymbolType string

const (
	TypeFunction  SymbolType = "function"
	TypeMethod    SymbolType = "method"
	TypeInterface SymbolType = "interface"
	TypeStruct    SymbolType = "struct"
	TypeVariable  SymbolType = "variable"
	TypeField     SymbolType = "field"
	TypeConstant  SymbolType = "constant"
	TypeType      SymbolType = "type"
)

// SymbolUse represents a single use of a symbol
type SymbolUse struct {
	File       string     `json:"file"`
	Line       int        `json:"line"`
	Column     int        `json:"column"`
	Context    UseContext `json:"context"`
	Snippet    string     `json:"snippet"`
	Type       SymbolType `json:"type"`
	SymbolName string     `json:"symbolName"`
	Receiver   string     `json:"receiver,omitempty"` // For method uses
}

// SymbolDefinition represents where a symbol is defined
type SymbolDefinition struct {
	Name       string     `json:"name"`
	Type       SymbolType `json:"type"`
	File       string     `json:"file"`
	Line       int        `json:"line"`
	Column     int        `json:"column"`
	Package    string     `json:"package"`
	Receiver   string     `json:"receiver,omitempty"` // For methods
	Signature  string     `json:"signature,omitempty"`
	IsExported bool       `json:"isExported"`
	Snippet    string     `json:"snippet"`
}

// SymbolQuery describes what symbol we're looking for
type SymbolQuery struct {
	Name          string     // Symbol name (e.g., "ValidateEmail")
	Type          SymbolType // What kind of symbol (optional, any if empty)
	Receiver      string     // For methods: receiver type (optional)
	Package       string     // Package scope (optional)
	FollowImports bool       // Search across imports (for future use)
}

// UseAnalyzer is the main analyzer for finding symbol uses
type UseAnalyzer struct {
	files           []string
	definitions     map[string]*SymbolDefinition
	uses            []SymbolUse
	fset            *token.FileSet
	fileASTs        map[string]*ast.File
	imports         map[string]string // Import alias -> actual package
	currentPackage  string
	currentFile     string
	receiverContext map[ast.Node]string // Maps method bodies to receiver types
}

// NewUseAnalyzer creates a new symbol analyzer
func NewUseAnalyzer(files []string) *UseAnalyzer {
	return &UseAnalyzer{
		files:           files,
		definitions:     make(map[string]*SymbolDefinition),
		uses:            []SymbolUse{},
		fset:            token.NewFileSet(),
		fileASTs:        make(map[string]*ast.File),
		imports:         make(map[string]string),
		receiverContext: make(map[ast.Node]string),
	}
}

// Parse loads and parses all files
func (ua *UseAnalyzer) Parse() error {
	for _, filePath := range ua.files {
		if !strings.HasSuffix(filePath, ".go") {
			continue
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		node, err := parser.ParseFile(ua.fset, filePath, content, parser.ParseComments)
		if err != nil {
			continue
		}

		ua.fileASTs[filePath] = node
	}

	return nil
}

// FindAllUses finds all uses of a symbol matching the query
func (ua *UseAnalyzer) FindAllUses(query SymbolQuery) ([]SymbolUse, error) {
	if err := ua.Parse(); err != nil {
		return nil, err
	}

	// First pass: collect all definitions
	ua.collectDefinitions()

	// Second pass: collect all uses
	ua.collectUses(query)

	return ua.uses, nil
}

// FindSymbolDefinition finds where a symbol is defined
func (ua *UseAnalyzer) FindSymbolDefinition(query SymbolQuery) (*SymbolDefinition, error) {
	if err := ua.Parse(); err != nil {
		return nil, err
	}

	ua.collectDefinitions()

	// First try exact key match
	key := ua.buildDefinitionKey(query.Name, query.Receiver)
	if def, exists := ua.definitions[key]; exists {
		return def, nil
	}

	// If no receiver specified, search for any definition matching the name
	if query.Receiver == "" {
		// Look for function with this name
		if def, exists := ua.definitions[query.Name]; exists {
			return def, nil
		}

		// Look for method with this name (any receiver)
		for defKey, def := range ua.definitions {
			if strings.HasSuffix(defKey, "."+query.Name) {
				return def, nil
			}
		}
	}

	return nil, fmt.Errorf("symbol not found: %s", query.Name)
}

// GetSymbolType returns the type of a symbol
func (ua *UseAnalyzer) GetSymbolType(name string) (SymbolType, error) {
	if err := ua.Parse(); err != nil {
		return "", err
	}

	ua.collectDefinitions()

	// Check for exact match first
	if def, exists := ua.definitions[name]; exists {
		return def.Type, nil
	}

	return "", fmt.Errorf("symbol type unknown: %s", name)
}

// FilterUsesByContext filters uses by their context
func FilterUsesByContext(uses []SymbolUse, contexts ...UseContext) []SymbolUse {
	if len(contexts) == 0 {
		return uses
	}

	contextMap := make(map[UseContext]bool)
	for _, ctx := range contexts {
		contextMap[ctx] = true
	}

	filtered := []SymbolUse{}
	for _, use := range uses {
		if contextMap[use.Context] {
			filtered = append(filtered, use)
		}
	}

	return filtered
}

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

// typeExprToString converts a type expression to a string
func (ua *UseAnalyzer) typeExprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + ua.typeExprToString(e.X)
	case *ast.SelectorExpr:
		return ua.typeExprToString(e.X) + "." + e.Sel.Name
	default:
		return ""
	}
}

// buildDefinitionKey creates a lookup key for a symbol
func (ua *UseAnalyzer) buildDefinitionKey(name, receiver string) string {
	if receiver != "" {
		return receiver + "." + name
	}
	return name
}

// getCodeSnippet extracts surrounding code context
func (ua *UseAnalyzer) getCodeSnippet(line int) string {
	if _, exists := ua.fileASTs[ua.currentFile]; exists {
		content, _ := os.ReadFile(ua.currentFile)
		lines := strings.Split(string(content), "\n")
		if line > 0 && line <= len(lines) {
			return strings.TrimSpace(lines[line-1])
		}
	}
	return ""
}

// recordUse adds a use to the collection, avoiding duplicates
func (ua *UseAnalyzer) recordUse(use SymbolUse) {
	// Simple deduplication based on file, line, and column
	for _, existing := range ua.uses {
		if existing.File == use.File && existing.Line == use.Line && existing.Column == use.Column {
			return
		}
	}
	ua.uses = append(ua.uses, use)
}
