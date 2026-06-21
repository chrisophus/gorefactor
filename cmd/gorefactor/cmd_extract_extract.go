package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// extractCommand implements `gorefactor extract <file> <startLine> <endLine> <methodName>`.
// It type-checks the enclosing package, derives parameter and return types
// for the selected block, synthesizes a new function, and rewrites the block
// as a call to it. Designed for free functions and methods at the body level;
// see the doc-comment caveats inside for v1 limitations.
func extractCommand(args []string) error {
	pos, flags := parseFlags(args, extractFlags)
	if len(pos) < 4 {
		return usageErrorf("usage: extract <file> <startLine> <endLine> <methodName>")
	}
	file := pos[0]
	m := &mutation{op: "extract", file: file}
	m.setCommonFlags(flags)
	startLine, err := strconv.Atoi(pos[1])
	if err != nil || startLine < 1 {
		return m.fail(usageErrorf("invalid startLine: %q", pos[1]))
	}
	endLine, err := strconv.Atoi(pos[2])
	if err != nil || endLine < startLine {
		return m.fail(usageErrorf("invalid endLine: %q", pos[2]))
	}
	methodName := pos[3]

	absFile, err := filepath.Abs(file)
	if err != nil {
		return m.fail(err)
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedCompiledGoFiles,
		Dir:   filepath.Dir(absFile),
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return m.fail(fmt.Errorf("load package: %w", err))
	}
	pkg, fileAST := findFileInPackages(pkgs, absFile)
	if pkg == nil {
		return m.fail(notFoundErrorf("file %s not in any loaded package", file))
	}
	fset := pkg.Fset

	enclosing, blockStmts, err := findExtractionTarget(fileAST, fset, startLine, endLine)
	if err != nil {
		return m.fail(notFoundErrorf("%v", err))
	}

	// Check for return statements with detailed error
	if containsReturn(blockStmts) {
		returnLines := findReturnLines(fset, blockStmts)
		err := ExampleReturnStatementError(file, startLine, endLine, returnLines)
		return m.fail(err)
	}

	params, returns, err := analyzeBlockTypes(pkg, fileAST, enclosing, blockStmts)
	if err != nil {
		// Wrap type analysis errors with DetailedError
		stderr := err.Error()
		
		// Check if it's an undefined variable/type error
		if strings.Contains(stderr, "undefined") || strings.Contains(stderr, "not defined") {
			detErr := NewDetailedError(ErrVariableOutOfScope, fmt.Sprintf("Cannot extract: %v", err)).
				WithContext(file, startLine, endLine, "Type analysis failed - undefined variables in extraction range").
				WithRootCause(stderr).
				WithSuggestion("expand_range",
					"Include variable definitions in extraction range (expand start line)",
					0.85).
				WithSuggestion("make_global",
					"Promote undefined variables to package level",
					0.30).
				WithDetail("error", stderr)
			return m.fail(detErr)
		}
		
		// Generic type error
		detErr := NewDetailedError(ErrTypeConflict, fmt.Sprintf("Cannot extract: %v", err)).
			WithContext(file, startLine, endLine, "Type analysis failed").
			WithRootCause(stderr).
			WithSuggestion("review_types",
				"Review variable types in extraction range",
				0.70).
			WithDetail("error", stderr)
		return m.fail(detErr)
	}

	newFunc, callSite, err := buildExtractedFunc(fset, methodName, blockStmts, params, returns)
	if err != nil {
		return m.fail(err)
	}

	return m.run(func() (string, error) {
		if err := rewriteExtraction(absFile, fset, enclosing, blockStmts, newFunc, callSite); err != nil {
			return "", err
		}
		return fmt.Sprintf("Extracted %s (params=%d, returns=%d)", methodName, len(params), len(returns)), nil
	})
}
