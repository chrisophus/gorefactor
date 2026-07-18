package parser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
)

// FileInfo represents the structure of a Go file
type FileInfo struct {
	Package    string      `json:"package"`
	Imports    []string    `json:"imports"`
	Functions  []Function  `json:"functions"`
	Methods    []Method    `json:"methods"`
	Structs    []Struct    `json:"structs"`
	Interfaces []Interface `json:"interfaces"`
}

// Function represents a function declaration
type Function struct {
	Name       string  `json:"name"`
	Parameters []Param `json:"parameters"`
	Results    []Param `json:"results"`
	StartLine  int     `json:"startLine"`
	EndLine    int     `json:"endLine"`
}

// Method represents a method declaration
type Method struct {
	Receiver   string  `json:"receiver"`
	Name       string  `json:"name"`
	Parameters []Param `json:"parameters"`
	Results    []Param `json:"results"`
	StartLine  int     `json:"startLine"`
	EndLine    int     `json:"endLine"`
}

// Param represents a function parameter or result
type Param struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Struct represents a struct type declaration
type Struct struct {
	Name   string  `json:"name"`
	Fields []Field `json:"fields"`
}

// Field represents a struct field
type Field struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Interface represents an interface type declaration
type Interface struct {
	Name     string   `json:"name"`
	Methods  []Method `json:"methods"`
	Embedded []string `json:"embedded,omitempty"`
}

// ParseFile parses a Go file and returns its structure
func ParseFile(filePath string) (*FileInfo, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}

	info := &FileInfo{
		Package: node.Name.Name,
	}

	// Parse imports
	for _, imp := range node.Imports {
		info.Imports = append(info.Imports, imp.Path.Value)
	}

	// Parse declarations
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil {
				// It's a method
				method := parseMethod(d, fset)
				info.Methods = append(info.Methods, method)
			} else {
				// It's a function
				function := parseFunction(d, fset)
				info.Functions = append(info.Functions, function)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					switch t := s.Type.(type) {
					case *ast.StructType:
						struct_ := parseStruct(s.Name.Name, t)
						info.Structs = append(info.Structs, struct_)
					case *ast.InterfaceType:
						interface_ := parseInterface(s.Name.Name, t)
						info.Interfaces = append(info.Interfaces, interface_)
					}
				}
			}
		}
	}

	return info, nil
}

func parseFunction(fn *ast.FuncDecl, fset *token.FileSet) Function {
	return Function{
		Name:       fn.Name.Name,
		Parameters: parseFieldList(fn.Type.Params),
		Results:    parseFieldList(fn.Type.Results),
		StartLine:  fset.Position(fn.Pos()).Line,
		EndLine:    fset.Position(fn.End()).Line,
	}
}

func parseMethod(fn *ast.FuncDecl, fset *token.FileSet) Method {
	receiver := ""
	if len(fn.Recv.List) > 0 {
		receiver = exprToString(fn.Recv.List[0].Type)
	}

	return Method{
		Receiver:   receiver,
		Name:       fn.Name.Name,
		Parameters: parseFieldList(fn.Type.Params),
		Results:    parseFieldList(fn.Type.Results),
		StartLine:  fset.Position(fn.Pos()).Line,
		EndLine:    fset.Position(fn.End()).Line,
	}
}

func parseStruct(name string, st *ast.StructType) Struct {
	// Reuse parseFieldList for the name/type walk; structs just relabel the
	// result as fields.
	fields := make([]Field, 0)
	for _, p := range parseFieldList(st.Fields) {
		fields = append(fields, Field(p))

	}
	return Struct{
		Name:   name,
		Fields: fields,
	}

}

func parseInterface(name string, it *ast.InterfaceType) Interface {
	methods := make([]Method, 0)
	embedded := make([]string, 0)
	for _, m := range it.Methods.List {
		if len(m.Names) > 0 {
			if fn, ok := m.Type.(*ast.FuncType); ok {
				method := Method{
					Name:       m.Names[0].Name,
					Parameters: parseFieldList(fn.Params),
					Results:    parseFieldList(fn.Results),
				}
				methods = append(methods, method)
			}
			continue
		}
		// No names: an embedded interface (or type-set term in a constraint).
		embedded = append(embedded, exprToString(m.Type))
	}
	if len(embedded) == 0 {
		embedded = nil
	}
	return Interface{
		Name:     name,
		Methods:  methods,
		Embedded: embedded,
	}
}

func parseFieldList(fl *ast.FieldList) []Param {
	if fl == nil {
		return nil
	}
	params := make([]Param, 0)
	for _, f := range fl.List {
		typeStr := exprToString(f.Type)
		if len(f.Names) == 0 {
			// Anonymous (unnamed) field: one entry with an empty name.
			params = append(params, Param{Name: "", Type: typeStr})
			continue
		}
		for _, n := range f.Names {
			params = append(params, Param{Name: n.Name, Type: typeStr})
		}
	}
	return params
}

// exprToString renders a type expression as Go source text. It delegates to go/types.ExprString,
// which handles every expression form (array lengths, variadics, func types, channels, generics)
// losslessly.
func exprToString(expr ast.Expr) string {
	return types.ExprString(expr)
}
