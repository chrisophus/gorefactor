package orchestrator

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"regexp"
	"strings"
)

// TargetLocation represents a location in the code
type TargetLocation struct {
	File      string `json:"file"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Function  string `json:"function,omitempty"`
	Method    string `json:"method,omitempty"`
}

// receiverTypeName returns the receiver type name of a method declaration
// ("" for plain functions). Pointer receivers are reported without the '*';
// generic receivers without their type arguments.
func ReceiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	switch e := t.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.IndexListExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// Fallback to simple string representation

func nodeToString(node ast.Node, fset *token.FileSet) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, node); err != nil {
		return fmt.Sprintf("%v", node)
	}
	return buf.String()
}

func scoreFuncName(node ast.Node, target *TargetSpecification) int {
	funcDecl, ok := node.(*ast.FuncDecl)
	if !ok {
		return 0
	}
	score := 0
	if target.FunctionName != "" && funcDecl.Name.Name == target.FunctionName {
		score += 10
	}
	if target.MethodName != "" && funcDecl.Name.Name == target.MethodName {
		score += 10
	}
	return score
}

func scoreTypeName(node ast.Node, target *TargetSpecification) int {
	if target.TypeName == "" {
		return 0
	}
	if typeSpec, ok := node.(*ast.TypeSpec); ok && typeSpec.Name.Name == target.TypeName {
		return 10
	}
	return 0
}

func scoreValueSpec(node ast.Node, target *TargetSpecification) int {
	if target.ConstName == "" && target.VarName == "" {
		return 0
	}
	valueSpec, ok := node.(*ast.ValueSpec)
	if !ok {
		return 0
	}
	for _, name := range valueSpec.Names {
		if name.Name == target.ConstName || name.Name == target.VarName {
			return 10
		}
	}
	return 0
}

func scoreCodePattern(node ast.Node, target *TargetSpecification, fset *token.FileSet) int {
	if target.CodePattern == "" {
		return 0
	}
	code := nodeToString(node, fset)
	matched, err := regexp.MatchString(target.CodePattern, code)
	if err == nil && matched {
		return 5
	}
	if strings.Contains(code, target.CodePattern) {
		return 3
	}
	return 0
}

func scoreVariableNames(node ast.Node, target *TargetSpecification) int {
	if len(target.VariableNames) == 0 {
		return 0
	}
	score := 0
	ast.Inspect(node, func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok {
			for _, varName := range target.VariableNames {
				if ident.Name == varName {
					score += 2
				}
			}
		}
		return true
	})
	return score
}

// ReceiverNone is the TargetSpecification.ReceiverType value that restricts
// matching to plain functions (no receiver). It disambiguates a top-level
// function from a method of the same name.
const ReceiverNone = "-"

func scoreFunctionCalls(node ast.Node, target *TargetSpecification) int {
	if len(target.FunctionCalls) == 0 {
		return 0
	}
	score := 0
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok {
			for _, funcName := range target.FunctionCalls {
				if ident.Name == funcName {
					score += 3
				}
			}
		}
		return true
	})
	return score
}

// receiverMatches reports whether a function declaration satisfies the
// receiverType constraint of a target specification. An empty constraint
// matches anything; ReceiverNone matches only plain functions.
func receiverMatches(fn *ast.FuncDecl, target *TargetSpecification) bool {
	if target.ReceiverType == "" {
		return true
	}
	recv := ReceiverTypeName(fn)
	if target.ReceiverType == ReceiverNone {
		return recv == ""
	}
	return recv == strings.TrimPrefix(target.ReceiverType, "*")
}

// semanticCandidate is one scored match found during semantic targeting.
type semanticCandidate struct {
	loc   *TargetLocation
	score int
	kind  string
}

func (c semanticCandidate) describe() string {
	name := c.loc.Function
	if name == "" {
		name = "(declaration)"
	}
	if c.loc.Method != "" {
		name = c.loc.Method + ":" + name
	}
	return fmt.Sprintf("%s %s (%s:%d)", c.kind, name, c.loc.File, c.loc.StartLine)
}
