package analyzer

import (
	"regexp"
	"strings"
)

var extractIdentifiersRe = regexp.MustCompile(`[a-zA-Z_][a-zA-Z0-9_]*`)

// Pattern detection methods
func (da *DiffAnalyzer) isFunctionAddition(code string) bool {
	return strings.Contains(code, "func ") && strings.Contains(code, "(") && strings.Contains(code, ")")
}

func (da *DiffAnalyzer) isMethodAddition(code string) bool {
	return strings.Contains(code, "func (") && strings.Contains(code, ") ") && !strings.Contains(code, "func ()")
}

func (da *DiffAnalyzer) isInterfaceAddition(code string) bool {
	return strings.Contains(code, "type ") && strings.Contains(code, "interface {")
}

func (da *DiffAnalyzer) isStructAddition(code string) bool {
	return strings.Contains(code, "type ") && strings.Contains(code, "struct {")
}

func (da *DiffAnalyzer) isFunctionRemoval(code string) bool {
	return strings.Contains(code, "func ") && strings.Contains(code, "(") && strings.Contains(code, ")")
}

func (da *DiffAnalyzer) isVariableRename(oldCode, newCode string) bool {
	// Trim whitespace for comparison
	oldCode = strings.TrimSpace(oldCode)
	newCode = strings.TrimSpace(newCode)

	// Check if codes differ in exactly one identifier
	oldIdents := da.extractIdentifiers(oldCode)
	newIdents := da.extractIdentifiers(newCode)

	// Count different identifiers
	oldSet := make(map[string]bool)
	newSet := make(map[string]bool)
	for _, id := range oldIdents {
		oldSet[id] = true
	}
	for _, id := range newIdents {
		newSet[id] = true
	}

	// If they have the same identifiers except for one difference, it's likely a rename
	oldOnly := []string{}
	newOnly := []string{}

	for id := range oldSet {
		if !newSet[id] {
			oldOnly = append(oldOnly, id)
		}
	}

	for id := range newSet {
		if !oldSet[id] {
			newOnly = append(newOnly, id)
		}
	}

	// Rename detected if exactly one identifier differs
	if len(oldOnly) == 1 && len(newOnly) == 1 {
		// Verify structure is similar by replacing the old identifier with the new one
		renamedCode := strings.ReplaceAll(oldCode, oldOnly[0], newOnly[0])
		if renamedCode == newCode {
			return true
		}
	}

	return false
}

// extractIdentifiers extracts all identifiers (variable names) from code
func (da *DiffAnalyzer) extractIdentifiers(code string) []string {
	re := extractIdentifiersRe
	matches := re.FindAllString(code, -1)
	// Deduplicate and return
	seen := make(map[string]bool)
	var result []string
	for _, m := range matches {
		if !seen[m] {
			result = append(result, m)
			seen[m] = true
		}
	}
	return result
}

func (da *DiffAnalyzer) isFunctionModification(oldCode, newCode string) bool {
	return strings.Contains(oldCode, "func ") && strings.Contains(newCode, "func ")
}

// extractByPattern returns the first capture group from pattern applied to code, or ""
func extractByPattern(pattern, code string) string {
	matches := regexp.MustCompile(pattern).FindStringSubmatch(code)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// Extraction methods
func (da *DiffAnalyzer) extractFunctionName(code string) string {
	return extractByPattern(`func\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\(`, code)
}

func (da *DiffAnalyzer) extractMethodName(code string) string {
	return extractByPattern(`func\s*\([^)]+\)\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\(`, code)
}

func (da *DiffAnalyzer) extractReceiverType(code string) string {
	return extractByPattern(`func\s*\([^*]*\*?([a-zA-Z_][a-zA-Z0-9_]*)\s*\)`, code)
}

func (da *DiffAnalyzer) extractInterfaceName(code string) string {
	return extractByPattern(`type\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+interface`, code)
}

func (da *DiffAnalyzer) extractStructName(code string) string {
	return extractByPattern(`type\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+struct`, code)
}

func (da *DiffAnalyzer) extractVariableName(code string) string {
	// First try assignment (x :=)
	if name := extractByPattern(`([a-zA-Z_][a-zA-Z0-9_]*)\s*:=`, code); name != "" {
		return name
	}

	// Fall back to extracting the single identifier from the code
	// (useful for variable uses like fmt.Println(varName))
	idents := da.extractIdentifiers(code)
	if len(idents) == 1 {
		return idents[0]
	}
	// If multiple identifiers, return the last one (usually the variable in question)
	if len(idents) > 1 {
		return idents[len(idents)-1]
	}

	return ""
}
