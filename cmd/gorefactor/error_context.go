package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ErrorCode categorizes errors for programmatic handling
type ErrorCode string

const (
	ErrVariableOutOfScope     ErrorCode = "VARIABLE_OUT_OF_SCOPE"
	ErrReturnStatementInBlock ErrorCode = "RETURN_IN_BLOCK"
	ErrInvalidRange           ErrorCode = "INVALID_RANGE"
	ErrFunctionNotFound       ErrorCode = "FUNCTION_NOT_FOUND"
	ErrImportCycle            ErrorCode = "IMPORT_CYCLE"
	ErrUnsafeExtraction       ErrorCode = "UNSAFE_EXTRACTION"
	ErrUnsafeMove             ErrorCode = "UNSAFE_MOVE"
	ErrTypeConflict           ErrorCode = "TYPE_CONFLICT"
	ErrMissingDependency      ErrorCode = "MISSING_DEPENDENCY"
	ErrParseError             ErrorCode = "PARSE_ERROR"
	ErrGeneric                ErrorCode = "GENERIC_ERROR"
	ErrScopeConflict          ErrorCode = "SCOPE_CONFLICT"
	ErrMultipleDefinitions    ErrorCode = "MULTIPLE_DEFINITIONS"
	ErrTargetNotFound         ErrorCode = "TARGET_NOT_FOUND"
	ErrCrossPackageMove       ErrorCode = "CROSS_PACKAGE_MOVE"
	ErrInvalidTarget          ErrorCode = "INVALID_TARGET"
	ErrInvalidSnippet         ErrorCode = "INVALID_SNIPPET"
	ErrInvalidLocation        ErrorCode = "INVALID_LOCATION"
	ErrHasCallers             ErrorCode = "HAS_CALLERS"
	ErrUnsafeDelete           ErrorCode = "UNSAFE_DELETE"
	ErrPatternNotFound        ErrorCode = "PATTERN_NOT_FOUND"
	ErrPatternAmbiguous       ErrorCode = "PATTERN_AMBIGUOUS"
)

// RecoverySuggestion proposes a way to fix the error
type RecoverySuggestion struct {
	// Approach: brief name for the suggestion
	Approach string `json:"approach"`

	// Description: human-readable explanation
	Description string `json:"description"`

	// Command: gorefactor command to try instead (optional)
	Command string `json:"command,omitempty"`

	// Likelihood: confidence that this will work (0.0-1.0)
	Likelihood float64 `json:"likelihood"`

	// Note: any caveats or additional info
	Note string `json:"note,omitempty"`
}

// ErrorContext provides location and code information
type ErrorContext struct {
	File        string `json:"file"`
	LineStart   int    `json:"lineStart"`
	LineEnd     int    `json:"lineEnd"`
	Description string `json:"description"`
}

// DetailedError provides structured error information for LLM consumption
type DetailedError struct {
	// Code: machine-readable error classification
	Code ErrorCode `json:"code"`

	// Message: human-readable error message
	Message string `json:"message"`

	// Details: context-specific information
	Details map[string]interface{} `json:"details,omitempty"`

	// Context: the problematic code/location
	Context *ErrorContext `json:"context,omitempty"`

	// RootCauses: why this error occurred
	RootCauses []string `json:"rootCauses,omitempty"`

	// Suggestions: ways to fix the problem (sorted by likelihood, descending)
	Suggestions []RecoverySuggestion `json:"suggestions,omitempty"`

	// RelatedCode: snippets of relevant code
	RelatedCode map[string]string `json:"relatedCode,omitempty"`
}

// NewDetailedError creates a DetailedError with sensible defaults
func NewDetailedError(code ErrorCode, message string) *DetailedError {
	return &DetailedError{
		Code:        code,
		Message:     message,
		Details:     make(map[string]interface{}),
		RootCauses:  []string{},
		Suggestions: []RecoverySuggestion{},
		RelatedCode: make(map[string]string),
	}
}

// MarshalJSON implements json.Marshaler
func (de *DetailedError) MarshalJSON() ([]byte, error) {
	type Alias DetailedError
	return json.Marshal(&struct {
		*Alias
		Code string `json:"code"`
	}{
		Alias: (*Alias)(de),
		Code:  string(de.Code),
	})
}

// Error implements the error interface
func (de *DetailedError) Error() string {
	return de.Message
}

// WithContext adds location information
func (de *DetailedError) WithContext(file string, lineStart, lineEnd int, desc string) *DetailedError {
	de.Context = &ErrorContext{
		File:        file,
		LineStart:   lineStart,
		LineEnd:     lineEnd,
		Description: desc,
	}
	return de
}

// WithRootCause adds an explanation
func (de *DetailedError) WithRootCause(cause string) *DetailedError {
	de.RootCauses = append(de.RootCauses, cause)
	return de
}

// WithSuggestion adds a recovery option (sorted by likelihood)
func (de *DetailedError) WithSuggestion(approach, description string, likelihood float64) *DetailedError {
	de.Suggestions = append(de.Suggestions, RecoverySuggestion{
		Approach:    approach,
		Description: description,
		Likelihood:  likelihood,
	})
	de.sortSuggestions()
	return de
}

// WithSuggestionCommand adds a recovery option with a command (sorted by likelihood)
func (de *DetailedError) WithSuggestionCommand(approach, description, command string, likelihood float64) *DetailedError {
	de.Suggestions = append(de.Suggestions, RecoverySuggestion{
		Approach:    approach,
		Description: description,
		Command:     command,
		Likelihood:  likelihood,
	})
	de.sortSuggestions()
	return de
}

// WithDetail adds context-specific information
func (de *DetailedError) WithDetail(key string, value interface{}) *DetailedError {
	de.Details[key] = value
	return de
}

// WithRelatedCode adds a code snippet
func (de *DetailedError) WithRelatedCode(label, code string) *DetailedError {
	de.RelatedCode[label] = code
	return de
}

// sortSuggestions sorts suggestions by likelihood (descending)
func (de *DetailedError) sortSuggestions() {
	// Simple bubble sort for small lists
	for i := 0; i < len(de.Suggestions); i++ {
		for j := i + 1; j < len(de.Suggestions); j++ {
			if de.Suggestions[j].Likelihood > de.Suggestions[i].Likelihood {
				de.Suggestions[i], de.Suggestions[j] = de.Suggestions[j], de.Suggestions[i]
			}
		}
	}
}

// IsDetailedError checks if an error is a DetailedError
func IsDetailedError(err error) bool {
	_, ok := err.(*DetailedError)
	return ok
}

// AsDetailedError casts an error to DetailedError if possible
func AsDetailedError(err error) *DetailedError {
	if de, ok := err.(*DetailedError); ok {
		return de
	}
	return nil
}

// ExampleVariableOutOfScopeError creates a detailed error for undefined variables
func ExampleVariableOutOfScopeError(file string, startLine, endLine int, undefinedVars []string, definitions map[string]int) *DetailedError {
	err := NewDetailedError(ErrVariableOutOfScope,
		fmt.Sprintf("Cannot extract: variable(s) not in scope: %v", undefinedVars))

	_ = err.WithContext(file, startLine, endLine,
		fmt.Sprintf("Extraction range %d-%d lacks these definitions: %v", startLine, endLine, undefinedVars))

	for _, varName := range undefinedVars {
		if defLine, ok := definitions[varName]; ok {
			_ = err.WithRootCause(
				fmt.Sprintf("%s is defined at line %d, outside extraction range %d-%d",
					varName, defLine, startLine, endLine))
		}
	}

	_ = err.WithSuggestionCommand("add_parameter",
		fmt.Sprintf("Add %v as parameter(s) to extracted function", undefinedVars),
		fmt.Sprintf("gorefactor change-signature %s <functionName> --add-param \"%s <Type>\"", file, undefinedVars[0]),
		0.95).
		WithSuggestion("expand_range",
			"Include variable definition(s) in extraction range (start at an earlier line)",
			0.80).
		WithSuggestion("make_global",
			"Promote variable(s) to package level if appropriate",
			0.30).
		WithDetail("undefinedVariables", undefinedVars).
		WithDetail("variableDefinitions", definitions)

	return err
}

// ExampleReturnStatementError creates a detailed error for return statements in extraction
func ExampleReturnStatementError(file string, startLine, endLine int, returnLines []int) *DetailedError {
	err := NewDetailedError(ErrReturnStatementInBlock,
		"Cannot extract: block contains return statement(s)")

	_ = err.WithContext(file, startLine, endLine,
		fmt.Sprintf("Extraction range includes return at line(s) %v", returnLines)).
		WithRootCause(
			"Return statements in extracted code are ambiguous - unclear whether to return from extracted function or caller").
		WithSuggestion("refactor_to_value_return",
			"Refactor to use a value return instead of early return",
			0.70).
		WithSuggestion("extract_narrower",
			"Extract a smaller block that doesn't include the return statement",
			0.80).
		WithSuggestion("extract_broader",
			"Extract the complete if/else block containing the return",
			0.60).
		WithDetail("returnLines", returnLines)

	return err
}

// ExampleTargetNotFoundError creates a detailed error for missing function/method
func ExampleTargetNotFoundError(file, targetName string) *DetailedError {
	err := NewDetailedError(ErrFunctionNotFound,
		fmt.Sprintf("Cannot find target: %s not found in %s", targetName, file))

	_ = err.WithContext(file, 0, 0,
		fmt.Sprintf("Function or method '%s' does not exist", targetName)).
		WithRootCause(
			fmt.Sprintf("%s is not defined in %s or was already deleted", targetName, file)).
		WithSuggestion("verify_name",
			"Verify the function/method name is spelled correctly",
			0.95).
		WithSuggestion("check_file",
			"Check that the target is in the correct file",
			0.90).
		WithSuggestionCommand("list_functions",
			"Use 'gorefactor list-functions <file>' to see available targets",
			fmt.Sprintf("gorefactor list-functions %s", file),
			0.85).
		WithDetail("targetName", targetName).
		WithDetail("file", file)

	return err
}

// ExampleImportCycleError creates a detailed error for circular imports
func ExampleImportCycleError(sourceFile, destFile, targetName string, cycle []string) *DetailedError {
	err := NewDetailedError(ErrImportCycle,
		fmt.Sprintf("Cannot move: would create circular import between %s and %s", sourceFile, destFile))

	_ = err.WithContext(sourceFile, 0, 0,
		fmt.Sprintf("Moving %s would create import cycle", targetName)).
		WithRootCause(
			fmt.Sprintf("%s imports from another file that %s depends on", destFile, sourceFile)).
		WithRootCause(
			fmt.Sprintf("Current cycle: %s", strings.Join(cycle, " → "))).
		WithSuggestion("move_dependencies_first",
			"First move the dependency types to a shared location",
			0.90).
		WithSuggestion("create_shared_types",
			"Extract shared types to a separate file both can import",
			0.85).
		WithSuggestion("consolidate_files",
			"Merge the two files if they're closely related",
			0.60).
		WithDetail("sourceFile", sourceFile).
		WithDetail("destFile", destFile).
		WithDetail("importCycle", cycle)

	return err
}
