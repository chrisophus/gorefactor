package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
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
		return usageErrorf("usage: extract <file> <startLine> <endLine> <methodName> [--allow-returns]")
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

	// Return statements that belong to the block itself (not to function
	// literals inside it) end the enclosing function, so a plain extraction
	// would change behavior. With --allow-returns they are lifted into a
	// (results..., done bool) helper instead of refused.
	rets := directReturns(blockStmts)
	allowReturns := flags["--allow-returns"] != ""
	if len(rets) > 0 && !allowReturns {
		returnLines := make([]int, 0, len(rets))
		for _, r := range rets {
			returnLines = append(returnLines, fset.Position(r.Pos()).Line)
		}
		return m.fail(ExampleReturnStatementError(file, startLine, endLine, returnLines))
	}

	// Improvement plan item 8: continue/break/goto that target an enclosing
	// scope cannot be extracted without restructuring the caller.
	if barriers := findJumpBarriers(fset, blockStmts); len(barriers) > 0 {
		return m.fail(notFoundErrorf("%v", jumpBarrierError(file, startLine, endLine, barriers)))
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

	var newFunc, callSite string
	if len(rets) > 0 {
		resultTypes, rerr := enclosingResultTypes(fset, enclosing)
		if rerr != nil {
			return m.fail(rerr)
		}
		if verr := validateReturnLift(fset, rets, len(resultTypes), returns); verr != nil {
			return m.fail(notFoundErrorf("cannot lift returns in lines %d-%d: %v", startLine, endLine, verr))
		}
		src, rerr := os.ReadFile(absFile)
		if rerr != nil {
			return m.fail(rerr)
		}
		// If the block is the function's tail, the original compiled only
		// because every path through the block returns, so `done` is always
		// true — the call site must unconditionally return the helper's values,
		// else the outer function falls off the end (missing return).
		isTail := blockIsFuncTail(blockStmts, enclosing)
		newFunc, callSite, err = buildReturnLiftedFunc(fset, methodName, blockStmts, params, rets, resultTypes, src, isTail)
	} else {
		newFunc, callSite, err = buildExtractedFunc(fset, methodName, blockStmts, params, returns)
	}
	if err != nil {
		return m.fail(err)
	}

	return m.run(func() (string, error) {
		if err := rewriteExtraction(absFile, fset, enclosing, blockStmts, newFunc, callSite); err != nil {
			return "", err
		}
		msg := fmt.Sprintf("Extracted %s (params=%d, returns=%d)", methodName, len(params), len(returns))
		if len(rets) > 0 {
			msg = fmt.Sprintf("Extracted %s (params=%d, lifted returns=%d)", methodName, len(params), len(rets))
		}
		if w := smallExtractionWarning(fset, methodName, blockStmts, startLine, endLine); w != "" {
			msg += "\n" + w
		}
		return msg, nil
	})

}

// smallExtractionWarning implements improvement plan item 3: when the requested
// range clips a statement boundary, the extractor silently trims to the nearest
// valid statements, sometimes capturing only a line or two. Warn when the result
// is suspiciously small so the caller can confirm the intended block was taken.
func smallExtractionWarning(fset *token.FileSet, methodName string, blockStmts []ast.Stmt, startLine, endLine int) string {
	if len(blockStmts) == 0 {
		return ""
	}
	extractedLines := fset.Position(blockStmts[len(blockStmts)-1].End()).Line -
		fset.Position(blockStmts[0].Pos()).Line + 1
	requestedLines := endLine - startLine + 1
	tooFewStmts := len(blockStmts) < 2
	muchSmaller := requestedLines >= 5 && extractedLines*100 < requestedLines*60 // >40% smaller
	if !tooFewStmts && !muchSmaller {
		return ""
	}
	return fmt.Sprintf(
		"Warning: extracted %s contains %d statement(s) spanning %d line(s), but the requested range was %d line(s). "+
			"The range was trimmed to statement boundaries — verify the intended block was captured.",
		methodName, len(blockStmts), extractedLines, requestedLines,
	)
}
