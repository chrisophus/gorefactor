package analyzer

import (
	"go/ast"
	"os"
	"strings"
)

// typeExprToString converts a type expression to a string
func (ua *UseAnalyzer) typeExprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + ua.typeExprToString(e.X)
	case *ast.SelectorExpr:
		return ua.typeExprToString(e.X) + "." + e.Sel.Name
	case *ast.ArrayType:
		if e.Len == nil {
			return "[]" + ua.typeExprToString(e.Elt)
		}
		return "[...]" + ua.typeExprToString(e.Elt)
	case *ast.MapType:
		return "map[" + ua.typeExprToString(e.Key) + "]" + ua.typeExprToString(e.Value)
	case *ast.ChanType:
		return "chan " + ua.typeExprToString(e.Value)
	case *ast.Ellipsis:
		return "..." + ua.typeExprToString(e.Elt)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func(...)"
	default:
		return ""
	}
}

// buildDefinitionKey creates a lookup key for a symbol
func (ua *UseAnalyzer) buildDefinitionKey(name, receiver string) string {
	if receiver != "" {
		return receiver + "." + name
	}
	return name
}

// getCodeSnippet extracts surrounding code context
func (ua *UseAnalyzer) getCodeSnippet(line int) string {
	if _, exists := ua.fileASTs[ua.currentFile]; exists {
		content, _ := os.ReadFile(ua.currentFile)
		lines := strings.Split(string(content), "\n")
		if line > 0 && line <= len(lines) {
			return strings.TrimSpace(lines[line-1])
		}
	}
	return ""
}

// recordUse adds a use to the collection, avoiding duplicates
func (ua *UseAnalyzer) recordUse(use SymbolUse) {
	// Simple deduplication based on file, line, and column
	for _, existing := range ua.uses {
		if existing.File == use.File && existing.Line == use.Line && existing.Column == use.Column {
			return
		}
	}
	ua.uses = append(ua.uses, use)
}

// filterUsesByContext filters uses by their context
func filterUsesByContext(uses []SymbolUse, contexts ...UseContext) []SymbolUse {
	if len(contexts) == 0 {
		return uses
	}

	contextMap := make(map[UseContext]bool)
	for _, ctx := range contexts {
		contextMap[ctx] = true
	}

	filtered := []SymbolUse{}
	for _, use := range uses {
		if contextMap[use.Context] {
			filtered = append(filtered, use)
		}
	}

	return filtered
}
