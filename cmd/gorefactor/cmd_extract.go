package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/chrisophus/gorefactor/refactor/extract"
)

// extractFlags: --allow-returns opts into lifting return-bearing blocks into a
// (results..., done bool) helper instead of refusing them. (Package-level var
// initializer — outside gorefactor's function-scoped edit commands.)
var extractFlags = mutFlagSpec(map[string]bool{"--allow-returns": false})

func init() {
	registerCommand(Command{
		Name:        "extract",
		Mutates:     true,
		MCPTool:     true,
		TxnSafe:     true,
		Description: "Extract a code block into a new function (--allow-returns lifts return-bearing blocks). Args: <file> <startLine> <endLine> <methodName>",
		Usage:       "extract <file> <startLine> <endLine> <methodName> [--allow-returns] [--json] [--dry-run] [--gate]",
		MinArgs:     4,
		MaxArgs:     4,
		Flags:       extractFlags,
		Run:         extractCommand,
	})
}

// extractCommand is a thin CLI wrapper over the refactor/extract engine: it
// parses flags, calls extract.PlanMethod, maps the engine's classified errors
// onto the CLI's rich DetailedError output, and applies the rewrite through the
// mutation lifecycle (dry-run / gate / JSON / undo).
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

	plan, err := extract.PlanMethod(file, startLine, endLine, methodName, flags["--allow-returns"] != "")
	if err != nil {
		return m.fail(extractMapError(err))
	}

	return m.run(func() (string, error) {
		if err := plan.Apply(); err != nil {
			return "", err
		}
		msg := fmt.Sprintf("Extracted %s (params=%d, returns=%d)", plan.MethodName, plan.NumParams, plan.NumReturns)
		if plan.LiftedReturns > 0 {
			msg = fmt.Sprintf("Extracted %s (params=%d, lifted returns=%d)", plan.MethodName, plan.NumParams, plan.LiftedReturns)
		}
		if plan.Warning != "" {
			msg += "\n" + plan.Warning
		}
		return msg, nil
	})
}

// extractMapError turns the engine's classified errors into the CLI's rich,
// LLM-oriented DetailedError payloads. Already-classified CLI errors (exit
// codes from cerr) pass through unchanged.
func extractMapError(err error) error {
	var refused *extract.ReturnsRefusedError
	if errors.As(err, &refused) {
		return ExampleReturnStatementError(refused.File, refused.StartLine, refused.EndLine, refused.ReturnLines)
	}
	var typeErr *extract.TypeAnalysisError
	if errors.As(err, &typeErr) {
		return extractWrapTypeAnalysisError(typeErr.Err, typeErr.File, typeErr.StartLine, typeErr.EndLine)
	}
	return err
}

// extractWrapTypeAnalysisError builds the rich DetailedError for a
// type-inference failure during extraction, distinguishing undefined-variable
// cases (expand the range) from general type conflicts.
func extractWrapTypeAnalysisError(err error, file string, startLine, endLine int) error {
	stderr := err.Error()

	if strings.Contains(stderr, "undefined") || strings.Contains(stderr, "not defined") {
		return NewDetailedError(ErrVariableOutOfScope, fmt.Sprintf("Cannot extract: %v", err)).
			WithContext(file, startLine, endLine, "Type analysis failed - undefined variables in extraction range").
			WithRootCause(stderr).
			WithSuggestion("expand_range",
				"Include variable definitions in extraction range (expand start line)",
				0.85).
			WithSuggestion("make_global",
				"Promote undefined variables to package level",
				0.30).
			WithDetail("error", stderr)
	}

	return NewDetailedError(ErrTypeConflict, fmt.Sprintf("Cannot extract: %v", err)).
		WithContext(file, startLine, endLine, "Type analysis failed").
		WithRootCause(stderr).
		WithSuggestion("review_types",
			"Review variable types in extraction range",
			0.70).
		WithDetail("error", stderr)
}
