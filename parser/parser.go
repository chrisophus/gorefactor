package parser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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
	Name    string   `json:"name"`
	Methods []Method `json:"methods"`
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
	fields := make([]Field, 0)
	for _, f := range st.Fields.List {
		fieldName := ""
		if len(f.Names) > 0 {
			fieldName = f.Names[0].Name
		}
		fields = append(fields, Field{
			Name: fieldName,
			Type: exprToString(f.Type),
		})
	}
	return Struct{
		Name:   name,
		Fields: fields,
	}
}

func parseInterface(name string, it *ast.InterfaceType) Interface {
	methods := make([]Method, 0)
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
		}
	}
	return Interface{
		Name:    name,
		Methods: methods,
	}
}

func parseFieldList(fl *ast.FieldList) []Param {
	if fl == nil {
		return nil
	}
	params := make([]Param, 0)
	for _, f := range fl.List {
		paramName := ""
		if len(f.Names) > 0 {
			paramName = f.Names[0].Name
		}
		params = append(params, Param{
			Name: paramName,
			Type: exprToString(f.Type),
		})
	}
	return params
}

func exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprToString(e.X) + "." + e.Sel.Name
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	case *ast.ArrayType:
		return "[]" + exprToString(e.Elt)
	case *ast.MapType:
		return "map[" + exprToString(e.Key) + "]" + exprToString(e.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.StructType:
		return "struct{}"
	default:
		return "unknown"
	}
}
