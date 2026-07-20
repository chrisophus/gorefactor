package analyzer

import (
	"go/ast"
	"go/token"
	"strings"
)

// TestOnlyLiveSymbol is an exported top-level symbol (function or type)
// declared in production code that is referenced only from _test.go files —
// never from non-test code in the module. It is advisory: an exported symbol
// can legitimately be public API consumed by other modules (invisible to this
// scan), so a finding is a signal to review, not a proven defect. The common
// real hit is a helper or type kept exported "for testing" that should be
// unexported, moved into a _test.go file, or deleted.
type TestOnlyLiveSymbol struct {
	Name string
	Kind string // "function" or "type"
	File string
	Line int
}

// DetectTestOnlyLiveSymbols reports exported top-level functions and types
// whose name appears in _test.go files but nowhere in non-test code outside
// their own declaration. Name frequency is counted module-wide (like
// DetectDeadExportedFunctions), so a cross-package same-named symbol biases
// the scan toward under-reporting — the safe direction for an advisory sensor.
// Methods are excluded: an exported method can satisfy an interface invoked
// via reflection with no same-name identifier in production code, so "only
// tests name it" would not imply "only tests use it".
func DetectTestOnlyLiveSymbols(files []string) []TestOnlyLiveSymbol {
	var prodFiles, testFiles []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			testFiles = append(testFiles, f)
		} else {
			prodFiles = append(prodFiles, f)
		}
	}
	if len(testFiles) == 0 || len(prodFiles) == 0 {
		return nil
	}
	prodFreq := identFreqAcross(prodFiles)
	testFreq := identFreqAcross(testFiles)

	declCount, decls := exportedTopLevelDecls(prodFiles)

	var out []TestOnlyLiveSymbol
	for _, d := range decls {
		// Referenced in production beyond its own declaration(s)? Then it is
		// not test-only. declCount is normally 1; a name declared once whose
		// production frequency is 1 appears only at its declaration site.
		if prodFreq[d.name] > declCount[d.name] {
			continue
		}
		if testFreq[d.name] == 0 {
			continue // referenced nowhere at all: dead-code's job, not this one
		}
		out = append(out, TestOnlyLiveSymbol{Name: d.name, Kind: d.kind, File: d.file, Line: d.line})
	}
	return out
}

type topLevelDecl struct {
	name string
	kind string
	file string
	line int
}

// exportedTopLevelDecls returns, for the given production files, the count of
// exported top-level func/type declarations per name and the declarations
// themselves. Methods and program entry points (main/init) and Go test
// function shapes are excluded.
func exportedTopLevelDecls(prodFiles []string) (map[string]int, []topLevelDecl) {
	declCount := map[string]int{}
	var decls []topLevelDecl
	for _, f := range prodFiles {
		content, err := readFileContent(f)
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		af, err := parseGoFile(fset, f, content)
		if err != nil {
			continue
		}
		for _, d := range af.Decls {
			switch g := d.(type) {
			case *ast.FuncDecl:
				if g.Recv != nil || !g.Name.IsExported() || isEntryOrTestFuncName(g.Name.Name) {
					continue
				}
				declCount[g.Name.Name]++
				decls = append(decls, topLevelDecl{g.Name.Name, "function", f, fset.Position(g.Pos()).Line})
			case *ast.GenDecl:
				if g.Tok != token.TYPE {
					continue
				}
				for _, s := range g.Specs {
					ts, ok := s.(*ast.TypeSpec)
					if !ok || !ts.Name.IsExported() {
						continue
					}
					declCount[ts.Name.Name]++
					decls = append(decls, topLevelDecl{ts.Name.Name, "type", f, fset.Position(ts.Pos()).Line})
				}
			}
		}
	}
	return declCount, decls
}

func isEntryOrTestFuncName(name string) bool {
	if name == "main" || name == "init" {
		return true
	}
	return strings.HasPrefix(name, "Test") ||
		strings.HasPrefix(name, "Benchmark") ||
		strings.HasPrefix(name, "Example") ||
		strings.HasPrefix(name, "Fuzz")
}

// identFreqAcross counts every identifier occurrence across the given files.
func identFreqAcross(files []string) map[string]int {
	freq := map[string]int{}
	for _, file := range files {
		content, err := readFileContent(file)
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		af, err := parseGoFile(fset, file, content)
		if err != nil {
			continue
		}
		ast.Inspect(af, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				freq[id.Name]++
			}
			return true
		})
	}
	return freq
}
