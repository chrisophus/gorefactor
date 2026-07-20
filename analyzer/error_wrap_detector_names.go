package analyzer

import (
	"go/ast"
	"strings"
	"unicode"
)

// isBareErrorIdent reports whether ident is a likely bare error value being
// returned without wrapping. Historically only the spelling "err" was flagged;
// that missed idiomatic names (e, retErr, cause). Without go/types we use
// name heuristics: common short forms, *Err/*Error suffixes, or an unexported
// name containing "err". nil is never an error value here.
func isBareErrorIdent(ident *ast.Ident) bool {
	if ident == nil || ident.Name == "nil" {
		return false
	}
	n := ident.Name
	switch n {
	case "err", "e", "er", "errno":
		return true
	}
	if strings.HasSuffix(n, "Err") || strings.HasSuffix(n, "Error") {
		return true
	}
	if ast.IsExported(n) {
		return false
	}
	lower := strings.ToLower(n)
	if !strings.Contains(lower, "err") {
		return false
	}
	// Reject pure type-ish names like "errorer" only if they look like types
	// (no digits, ends with "er" after err) — keep it inclusive; false
	// positives are cheaper than missed wraps for an autofixable rule.
	for _, r := range n {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return true
}
