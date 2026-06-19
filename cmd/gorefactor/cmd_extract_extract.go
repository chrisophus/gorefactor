package main

import (
	"fmt"
	"path/filepath"
	"strconv"

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
	if containsReturn(blockStmts) {
		return m.fail(fmt.Errorf("block contains a return statement; v1 extract does not handle this"))
	}

	params, returns, err := analyzeBlockTypes(pkg, fileAST, enclosing, blockStmts)
	if err != nil {
		return m.fail(err)
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
