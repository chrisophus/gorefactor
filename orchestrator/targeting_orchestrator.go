package orchestrator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

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

func (o *Orchestrator) calculateSemanticScore(node ast.Node, target *TargetSpecification, fset *token.FileSet) int {
	return scoreFuncName(node, target) +
		scoreTypeName(node, target) +
		scoreValueSpec(node, target) +
		scoreCodePattern(node, target, fset) +
		scoreVariableNames(node, target) +
		scoreFunctionCalls(node, target)
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
	loc.Method = ReceiverTypeName(funcDecl)
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
