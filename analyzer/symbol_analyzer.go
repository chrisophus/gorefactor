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
	parsed          bool                // Parse() is idempotent; guards re-parsing
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

// Parse loads and parses all files. It is idempotent: repeated calls reuse the
// ASTs parsed on the first call (callers such as FindCallers invoke it per query).
func (ua *UseAnalyzer) Parse() error {
	if ua.parsed {
		return nil
	}
	ua.parsed = true
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

// SeedASTs injects a pre-parsed FileSet and per-file ASTs, marking the analyzer
// as already parsed so a subsequent Parse() is a no-op. It lets a long-lived
// caller (e.g. the MCP server) parse each file once into a shared cache and
// reuse the ASTs across many queries instead of re-reading and re-parsing every
// file on every call. The FileSet must be the one the ASTs were parsed with, so
// position lookups stay valid. asts maps file path -> parsed file.
func (ua *UseAnalyzer) SeedASTs(fset *token.FileSet, asts map[string]*ast.File) {
	ua.fset = fset
	ua.fileASTs = asts
	ua.parsed = true
}

// FindAllUses finds all uses of a symbol matching the query
func (ua *UseAnalyzer) FindAllUses(query SymbolQuery) ([]SymbolUse, error) {
	if err := ua.Parse(); err != nil {
		return nil, fmt.Errorf(

			// First pass: collect all definitions
			"parse: %w", err)
	}

	ua.collectDefinitions()

	// Second pass: collect all uses
	ua.collectUses(query)

	return ua.uses, nil
}

// FindSymbolDefinition finds where a symbol is defined
func (ua *UseAnalyzer) FindSymbolDefinition(query SymbolQuery) (*SymbolDefinition, error) {
	if err := ua.Parse(); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
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
		return "", fmt.Errorf("parse: %w", err)
	}

	ua.collectDefinitions()

	// Check for exact match first
	if def, exists := ua.definitions[name]; exists {
		return def.Type, nil
	}

	return "", fmt.Errorf("symbol type unknown: %s", name)
}
