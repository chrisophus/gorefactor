package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// parseNonTestFiles parses every non-test file in files, silently skipping unparsable ones, and
// returns the survivors with their paths.
func parseNonTestFiles(files []string) (*token.FileSet, []*ast.File, []string) {
	fset := token.NewFileSet()
	var astFiles []*ast.File
	var paths []string
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			continue
		}
		astFiles = append(astFiles, f)
		paths = append(paths, path)
	}
	return fset, astFiles, paths
}

// PackageErrorSentinels reports the package-level `errors.New` sentinel var names declared across
// files (test files excluded), keyed by name.
func PackageErrorSentinels(files []string) (map[string]bool, error) {
	_, astFiles, paths := parseNonTestFiles(files)
	return collectErrorsNewSentinels(astFiles, paths), nil

}

// ApplySentinelWrapFixes wraps every bare return of sentinel in src with
// fmt.Errorf("<enclosing function>: %w", sentinel) — the transform the
// duplicate-bare-sentinel rule prescribes. errors.Is against the sentinel
// keeps working through %w; direct == comparisons in callers do not, which
// is exactly what the rule exists to surface (and what a verify gate
// catches). Returns the rewritten source and the number of return results
// wrapped; nil output when nothing matched.
func ApplySentinelWrapFixes(filename string, src []byte, sentinel string) ([]byte, int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, 0, fmt.Errorf("parse %s: %w", filename, err)
	}
	var edits []srcEdit
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		context := camelWords(fn.Name.Name)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok {
				return true
			}
			for _, r := range ret.Results {
				id, ok := r.(*ast.Ident)
				if !ok || id.Name != sentinel {
					continue
				}
				start := fset.Position(id.Pos()).Offset
				end := fset.Position(id.End()).Offset
				edits = append(edits, srcEdit{start: start, end: end, repl: fmt.Sprintf("fmt.Errorf(%q, %s)", context+": %w", sentinel)})
			}
			return true
		})
	}
	if len(edits) == 0 {
		return nil, 0, nil
	}
	return applySrcEdits(src, edits), len(edits), nil
}
