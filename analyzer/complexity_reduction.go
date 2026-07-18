package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
)

// ComplexityExtraction is one suggested sub-block to extract in order to bring
// an over-threshold function below the complexity threshold.
type ComplexityExtraction struct {
	StartLine  int    `json:"startLine"`
	EndLine    int    `json:"endLine"`
	Complexity int    `json:"complexity"` // complexity points shed by extracting this block
	Branches   int    `json:"branches"`   // control structures inside the block
	Suggestion string `json:"suggestion"` // placeholder helper name
}

// ComplexityReduction is the result of RecommendComplexityReduction: the target
// function's current complexity, the threshold, the greedily-chosen extractions,
// and the projected remaining complexity after applying them.
type ComplexityReduction struct {
	Function    string                 `json:"function"`
	Complexity  int                    `json:"complexity"`
	Threshold   int                    `json:"threshold"`
	Projected   int                    `json:"projected"`
	Extractions []ComplexityExtraction `json:"extractions"`
}

// RecommendComplexityReduction implements improvement plan item 7. Given a
// function that exceeds threshold, it finds the minimum set of top-level
// statement blocks whose extraction brings the parent below threshold — unlike
// the default recommend, which surfaces micro-blocks that maximize local
// reduction per line and can leave the parent still over threshold.
//
// The algorithm: score each top-level statement in the body by its complexity
// contribution, sort descending, and greedily pick blocks until the projected
// remainder (total minus shed points) is at or below the threshold.
func RecommendComplexityReduction(filePath, functionName string, threshold int) (*ComplexityReduction, error) {
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}
	cmap := ast.NewCommentMap(fset, astFile, astFile.Comments)
	var target *ast.FuncDecl
	for _, decl := range astFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if fn.Name.Name == functionName {
			target = fn
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("function %q not found in %s", functionName, filePath)
	}

	total := calculateFunctionComplexity(target)
	result := &ComplexityReduction{
		Function:   functionName,
		Complexity: total,
		Threshold:  threshold,
		Projected:  total,
	}
	if total <= threshold {
		return result, nil // already under threshold; nothing to shed
	}

	// Score each top-level statement by its complexity contribution.
	type candidate struct {
		stmt       ast.Stmt
		contrib    int
		branches   int
		start, end int
	}
	var candidates []candidate
	for _, stmt := range target.Body.List {
		contrib := 0
		countComplexity(stmt, &contrib)
		if contrib == 0 {
			continue // straight-line statement, extracting it sheds nothing
		}
		if contrib+1 > threshold {
			// A helper carries base complexity 1 plus the block contribution.
			// If that already exceeds the threshold, extracting only relocates
			// the finding into the helper; skip the block as vacuous.
			continue
		}
		candidates = append(candidates, candidate{
			stmt:     stmt,
			contrib:  contrib,
			branches: countControlStructures(stmt),
			start:    fset.Position(stmt.Pos()).Line,
			end:      fset.Position(stmt.End()).Line,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].contrib > candidates[j].contrib
	})

	projected := total
	used := PackageFuncNames(filePath)
	for _, c := range candidates {
		if projected <= threshold {
			break
		}
		result.Extractions = append(result.Extractions, ComplexityExtraction{
			StartLine:  c.start,
			EndLine:    c.end,
			Complexity: c.contrib,
			Branches:   c.branches,
			Suggestion: SuggestBlockName(c.stmt, cmap, c.start, used),
		})
		projected -= c.contrib
	}
	result.Projected = projected
	return result, nil
}
