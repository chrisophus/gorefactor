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

// ReceiverNone is the TargetSpecification.ReceiverType value that restricts
// matching to plain functions (no receiver). It disambiguates a top-level
// function from a method of the same name.
const ReceiverNone = "-"

// receiverTypeName returns the receiver type name of a method declaration
// ("" for plain functions). Pointer receivers are reported without the '*';
// generic receivers without their type arguments.
func receiverTypeName(fn *ast.FuncDecl) string {
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

// receiverMatches reports whether a function declaration satisfies the
// receiverType constraint of a target specification. An empty constraint
// matches anything; ReceiverNone matches only plain functions.
func receiverMatches(fn *ast.FuncDecl, target *TargetSpecification) bool {
	if target.ReceiverType == "" {
		return true
	}
	recv := receiverTypeName(fn)
	if target.ReceiverType == ReceiverNone {
		return recv == ""
	}
	return recv == strings.TrimPrefix(target.ReceiverType, "*")
}

func (o *Orchestrator) matchFuncDecl(funcDecl *ast.FuncDecl, target *TargetSpecification, fset *token.FileSet, filePath string) (*TargetLocation, int) {
	// receiverType is a hard constraint: when specified, declarations with a
	// non-matching receiver are disqualified outright so that
	// "Receiver:Method" style targets are unambiguous.
	if !receiverMatches(funcDecl, target) {
		return nil, 0
	}
	score := o.calculateSemanticScore(funcDecl, target, fset)
	if score == 0 {
		return nil, 0
	}
	if target.ReceiverType != "" && target.ReceiverType != ReceiverNone {
		score += 10
	}
	loc := &TargetLocation{
		File:      filePath,
		StartLine: fset.Position(funcDecl.Pos()).Line,
		EndLine:   fset.Position(funcDecl.End()).Line,
		Function:  funcDecl.Name.Name,
	}
	loc.Method = receiverTypeName(funcDecl)
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

func (o *Orchestrator) findTargetBySemantics(target *TargetSpecification, filePath string) (*TargetLocation, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}
	var candidates []semanticCandidate
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if funcDecl, ok := n.(*ast.FuncDecl); ok {
			if m, s := o.matchFuncDecl(funcDecl, target, fset, filePath); s > 0 {
				kind := "func"
				if funcDecl.Recv != nil {
					kind = "method"
				}
				candidates = append(candidates, semanticCandidate{loc: m, score: s, kind: kind})
			}
		}
		if genDecl, ok := n.(*ast.GenDecl); ok {
			if m, s := o.matchGenDecl(genDecl, target, fset, filePath); s > 0 {
				candidates = append(candidates, semanticCandidate{loc: m, score: s, kind: "decl"})
			}
		}
		return true
	})
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no suitable target found using semantic matching")
	}

	best := candidates[0]
	var tied []semanticCandidate
	for _, c := range candidates {
		switch {
		case c.score > best.score:
			best = c
			tied = tied[:0]
		case c.score == best.score && c.loc.StartLine != best.loc.StartLine:
			tied = append(tied, c)
		}
	}
	if len(tied) > 0 {
		all := append([]semanticCandidate{best}, tied...)
		var lines []string
		for _, c := range all {
			lines = append(lines, "  "+c.describe())
		}
		return nil, fmt.Errorf(
			"ambiguous target: %d candidates tie with score %d:\n%s\ndisambiguate with receiverType (use %q for a plain function) or a more specific codePattern",
			len(all), best.score, strings.Join(lines, "\n"), ReceiverNone)
	}
	return best.loc, nil
}
