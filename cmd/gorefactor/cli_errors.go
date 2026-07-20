package main

// Semantic error classification now lives in internal/cerr so the importable
// refactoring engines (refactor/extract, refactor/changesig) can produce the
// same exit-code-carrying errors without importing package main. These aliases
// keep the historical spellings used throughout the CLI unchanged.

import "github.com/chrisophus/gorefactor/internal/cerr"

const (
	exitOK          = cerr.ExitOK
	exitUsage       = cerr.ExitUsage
	exitNotFound    = cerr.ExitNotFound
	exitParseError  = cerr.ExitParseError
	exitGateFailure = cerr.ExitGateFailure
)

// cliError is retained as an alias so existing type assertions keep compiling.
type cliError = cerr.CLIError

var (
	usageErrorf    = cerr.Usagef
	parseErrorf    = cerr.Parsef
	gateErrorf     = cerr.Gatef
	notFoundErrorf = cerr.NotFoundf
	notFoundError  = cerr.NotFound
	exitCodeFor    = cerr.ExitCodeFor
	errCandidates  = cerr.Candidates
	closestMatch   = cerr.ClosestMatch
)
