package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// minExtractableBlockLines keeps the greedy length reducer from extracting
// trivial statements: a block must span at least this many lines before
// pulling it out actually shortens the parent meaningfully.
const minExtractableBlockLines = 5

// LengthExtraction is one suggested sub-block to extract in order to bring an
// over-threshold function below the line-count threshold.
type LengthExtraction struct {
	StartLine  int    `json:"startLine"`
	EndLine    int    `json:"endLine"`
	Lines      int    `json:"lines"`      // lines shed by extracting this block (span minus the call line)
	Suggestion string `json:"suggestion"` // placeholder helper name
}

// LengthReduction is the result of RecommendLengthReduction: the target
// function's current length, the threshold, the greedily-chosen extractions,
// and the projected remaining length after applying them.
type LengthReduction struct {
	Function    string             `json:"function"`
	Lines       int                `json:"lines"`
	Threshold   int                `json:"threshold"`
	Projected   int                `json:"projected"`
	Extractions []LengthExtraction `json:"extractions"`
}

const DefaultLongFunctionLines = 75

// RecommendLengthReduction is the line-count analog of
// RecommendComplexityReduction: given a function or method (locator "Name" or
// "Receiver:Name") that exceeds maxLines, it greedily picks the minimum set
// of top-level statement blocks whose extraction brings the parent under
// maxLines. Each extracted block is replaced by a single call line, so a
// block spanning N lines sheds N-1.
func RecommendLengthReduction(filePath, locator string, maxLines int) (*LengthReduction, error) {
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}
	cmap := ast.NewCommentMap(fset, astFile, astFile.Comments)
	target := findFuncByLocator(astFile, locator)
	if target == nil {
		return nil, fmt.Errorf("function %q not found in %s", locator, filePath)
	}

	total := fset.Position(target.End()).Line - fset.Position(target.Pos()).Line + 1
	result := &LengthReduction{
		Function:  locator,
		Lines:     total,
		Threshold: maxLines,
		Projected: total,
	}
	if total <= maxLines {
		return result, nil // already under threshold; nothing to shed
	}

	type candidate struct {
		stmt       ast.Stmt
		shed       int
		start, end int
	}
	var candidates []candidate
	for _, stmt := range target.Body.List {
		start := fset.Position(stmt.Pos()).Line
		end := fset.Position(stmt.End()).Line
		span := end - start + 1
		if span < minExtractableBlockLines {
			continue
		}
		if span+2 >= helperLineBudget(maxLines) {
			continue
		}

		candidates = append(candidates, candidate{stmt: stmt, shed: span - 1, start: start, end: end})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].shed > candidates[j].shed
	})

	projected := total
	used := PackageFuncNames(filePath)
	for _, c := range candidates {
		if projected <= maxLines {
			break
		}
		result.Extractions = append(result.Extractions, LengthExtraction{
			StartLine:  c.start,
			EndLine:    c.end,
			Lines:      c.shed,
			Suggestion: SuggestBlockName(c.stmt, cmap, c.start, used),
		})
		projected -= c.shed
	}
	result.Projected = projected
	return result, nil
}

func helperLineBudget(maxLines int) int {
	if maxLines < DefaultLongFunctionLines {
		return maxLines
	}
	return DefaultLongFunctionLines
}

// findFuncByLocator matches a *ast.FuncDecl by "Name" or "Receiver:Name"
// (pointer receivers match without the star, mirroring the CLI locator
// convention).
func findFuncByLocator(astFile *ast.File, locator string) *ast.FuncDecl {
	recv, name := "", locator
	if i := strings.IndexByte(locator, ':'); i >= 0 {
		recv, name = locator[:i], locator[i+1:]
	}
	for _, decl := range astFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Name.Name != name {
			continue
		}
		if recv == "" && fn.Recv == nil {
			return fn
		}
		if recv != "" && fn.Recv != nil && receiverTypeName(fn) == recv {
			return fn
		}
	}
	return nil
}
