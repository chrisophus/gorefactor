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

		// Check function declarations
		if funcDecl, ok := n.(*ast.FuncDecl); ok {
			score := o.calculateSemanticScore(funcDecl, target, fset)
			if score > bestScore {
				startLine := fset.Position(funcDecl.Pos()).Line
				endLine := fset.Position(funcDecl.End()).Line
				bestMatch = &TargetLocation{
					File:      filePath,
					StartLine: startLine,
					EndLine:   endLine,
					Function:  funcDecl.Name.Name,
				}
				if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
					if t, ok := funcDecl.Recv.List[0].Type.(*ast.StarExpr); ok {
						if ident, ok := t.X.(*ast.Ident); ok {
							bestMatch.Method = ident.Name
						}
					}
				}
				bestScore = score
			}
		}

		// Check type declarations
		if genDecl, ok := n.(*ast.GenDecl); ok {
			// First check if the entire GenDecl matches (for code patterns)
			genDeclScore := o.calculateSemanticScore(genDecl, target, fset)
			if genDeclScore > bestScore {
				startLine := fset.Position(genDecl.Pos()).Line
				endLine := fset.Position(genDecl.End()).Line
				bestMatch = &TargetLocation{
					File:      filePath,
					StartLine: startLine,
					EndLine:   endLine,
					Function:  "", // Will be set below if we find a specific spec
				}
				bestScore = genDeclScore
			}

			for _, spec := range genDecl.Specs {
				if typeSpec, ok := spec.(*ast.TypeSpec); ok {
					score := o.calculateSemanticScore(typeSpec, target, fset)
					if score > bestScore {
						startLine := fset.Position(genDecl.Pos()).Line
						endLine := fset.Position(genDecl.End()).Line
						bestMatch = &TargetLocation{
							File:      filePath,
							StartLine: startLine,
							EndLine:   endLine,
							Function:  typeSpec.Name.Name, // Reuse Function field for type name
						}
						bestScore = score
					}
				}

				// Check const/var declarations
				if valueSpec, ok := spec.(*ast.ValueSpec); ok {
					for _, name := range valueSpec.Names {
						score := o.calculateSemanticScore(valueSpec, target, fset)
						if score > bestScore {
							startLine := fset.Position(genDecl.Pos()).Line
							endLine := fset.Position(genDecl.End()).Line
							bestMatch = &TargetLocation{
								File:      filePath,
								StartLine: startLine,
								EndLine:   endLine,
								Function:  name.Name, // Reuse Function field for const/var name
							}
							bestScore = score
						}
					}
				}
			}
		}

		return true
	})

	if bestMatch != nil && bestScore > 0 {
		return bestMatch, nil
	}

	return nil, fmt.Errorf("no suitable target found using semantic matching")
}

// calculateSemanticScore calculates how well a node matches the target specification
func (o *Orchestrator) calculateSemanticScore(node ast.Node, target *TargetSpecification, fset *token.FileSet) int {
	score := 0

	// Check function name match
	if target.FunctionName != "" {
		if funcDecl, ok := node.(*ast.FuncDecl); ok {
			if funcDecl.Name.Name == target.FunctionName {
				score += 10
			}
		}
	}

	// Check method name match
	if target.MethodName != "" {
		if funcDecl, ok := node.(*ast.FuncDecl); ok {
			if funcDecl.Name.Name == target.MethodName {
				score += 10
			}
		}
	}

	// Check type name match
	if target.TypeName != "" {
		if typeSpec, ok := node.(*ast.TypeSpec); ok {
			if typeSpec.Name.Name == target.TypeName {
				score += 10
			}
		}
	}

	// Check const name match
	if target.ConstName != "" {
		if valueSpec, ok := node.(*ast.ValueSpec); ok {
			for _, name := range valueSpec.Names {
				if name.Name == target.ConstName {
					score += 10
					break
				}
			}
		}
	}

	// Check var name match
	if target.VarName != "" {
		if valueSpec, ok := node.(*ast.ValueSpec); ok {
			for _, name := range valueSpec.Names {
				if name.Name == target.VarName {
					score += 10
					break
				}
			}
		}
	}

	// Check code pattern match with regex support
	if target.CodePattern != "" {
		code := o.nodeToString(node, fset)

		// Try regex first, fall back to simple contains
		matched, err := regexp.MatchString(target.CodePattern, code)
		if err == nil && matched {
			score += 5
		} else if strings.Contains(code, target.CodePattern) {
			score += 3 // Lower score for non-regex match
		}
	}

	// Check variable names
	if len(target.VariableNames) > 0 {
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
	}

	// Check function calls

	// nodeToString converts an AST node to a string representation

	// Fallback to simple string representation

	if len(target.FunctionCalls) > 0 {
		ast.Inspect(node, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if ident, ok := call.Fun.(*ast.Ident); ok {
					for _, funcName := range target.FunctionCalls {
						if ident.Name == funcName {
							score += 3
						}
					}
				}
			}
			return true
		})
	}

	return score
}

func (o *Orchestrator) nodeToString(node ast.Node, fset *token.FileSet) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, node); err != nil {

		return fmt.Sprintf("%v", node)
	}
	return buf.String()
}
