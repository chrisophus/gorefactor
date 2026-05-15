package orchestrator

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
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

// findTarget uses resilient targeting to locate the target for refactoring
func (o *Orchestrator) findTarget(target *TargetSpecification, filePath string) (*TargetLocation, error) {
	if target == nil {
		return nil, fmt.Errorf("no target specification provided")
	}

	// If line-based targeting is provided, use it directly
	if target.StartLine != nil && target.EndLine != nil {
		return &TargetLocation{
			File:      filePath,
			StartLine: *target.StartLine,
			EndLine:   *target.EndLine,
		}, nil
	}

	// Use semantic targeting
	return o.findTargetBySemantics(target, filePath)
}

// findTargetBySemantics uses semantic information to find the target

// Check function declarations

// Check type declarations

// First check if the entire GenDecl matches (for code patterns)

// Will be set below if we find a specific spec

// Reuse Function field for type name

// Check const/var declarations

// Reuse Function field for const/var name

// calculateSemanticScore calculates how well a node matches the target specification

// Check function name match

// Check method name match

// Check type name match

// Check const name match

// Check var name match

// Check code pattern match with regex support

// Try regex first, fall back to simple contains

// Lower score for non-regex match

// Check variable names

// Check function calls

// nodeToString converts an AST node to a string representation

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
func (o *Orchestrator) calculateSemanticScore(node ast.Node, target *TargetSpecification, fset *token.FileSet) int {
	return scoreFuncName(node, target) +
		scoreTypeName(node, target) +
		scoreValueSpec(node, target) +
		scoreCodePattern(node, target, fset) +
		scoreVariableNames(node, target) +
		scoreFunctionCalls(node, target)
}
func (o *Orchestrator) matchFuncDecl(funcDecl *ast.FuncDecl, target *TargetSpecification, fset *token.FileSet, filePath string) (*TargetLocation, int) {
	score := o.calculateSemanticScore(funcDecl, target, fset)
	if score == 0 {
		return nil, 0
	}
	loc := &TargetLocation{
		File:      filePath,
		StartLine: fset.Position(funcDecl.Pos()).Line,
		EndLine:   fset.Position(funcDecl.End()).Line,
		Function:  funcDecl.Name.Name,
	}
	if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
		if t, ok := funcDecl.Recv.List[0].Type.(*ast.StarExpr); ok {
			if ident, ok := t.X.(*ast.Ident); ok {
				loc.Method = ident.Name
			}
		}
	}
	return loc, score
}
func (o *Orchestrator) matchGenDecl(genDecl *ast.GenDecl, target *TargetSpecification, fset *token.FileSet, filePath string) (*TargetLocation, int) {
	startLine := fset.Position(genDecl.Pos()).Line
	endLine := fset.Position(genDecl.End()).Line
	bestScore := o.calculateSemanticScore(genDecl, target, fset)
	var bestMatch *TargetLocation
	if bestScore > 0 {
		bestMatch = &TargetLocation{File: filePath, StartLine: startLine, EndLine: endLine}
	}
	for _, spec := range genDecl.Specs {
		if typeSpec, ok := spec.(*ast.TypeSpec); ok {
			if s := o.calculateSemanticScore(typeSpec, target, fset); s > bestScore {
				bestScore = s
				bestMatch = &TargetLocation{File: filePath, StartLine: startLine, EndLine: endLine, Function: typeSpec.Name.Name}
			}
		}
		if valueSpec, ok := spec.(*ast.ValueSpec); ok {
			for _, name := range valueSpec.Names {
				if s := o.calculateSemanticScore(valueSpec, target, fset); s > bestScore {
					bestScore = s
					bestMatch = &TargetLocation{File: filePath, StartLine: startLine, EndLine: endLine, Function: name.Name}
				}
			}
		}
	}
	return bestMatch, bestScore
}
func (o *Orchestrator) findTargetBySemantics(target *TargetSpecification, filePath string) (*TargetLocation, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}
	var bestMatch *TargetLocation
	var bestScore int
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if funcDecl, ok := n.(*ast.FuncDecl); ok {
			if m, s := o.matchFuncDecl(funcDecl, target, fset, filePath); s > bestScore {
				bestMatch, bestScore = m, s
			}
		}
		if genDecl, ok := n.(*ast.GenDecl); ok {
			if m, s := o.matchGenDecl(genDecl, target, fset, filePath); s > bestScore {
				bestMatch, bestScore = m, s
			}
		}
		return true
	})
	if bestMatch != nil && bestScore > 0 {
		return bestMatch, nil
	}
	return nil, fmt.Errorf("no suitable target found using semantic matching")
}
