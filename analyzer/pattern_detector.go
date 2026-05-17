package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// PatternDetector identifies architectural patterns and code smells
type PatternDetector struct {
	file *ast.File
}

// ArchitecturalPattern represents a detected pattern or smell
type ArchitecturalPattern struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`     // "smell" or "pattern"
	Severity    string   `json:"severity"` // low, medium, high
	Description string   `json:"description"`
	Affected    []string `json:"affected"` // function/type names
	Suggestion  string   `json:"suggestion"`
}

// NewPatternDetector creates a detector for a file
func NewPatternDetector(file *ast.File) *PatternDetector {
	return &PatternDetector{file: file}
}

// DetectPatterns finds all architectural patterns and smells
func (pd *PatternDetector) DetectPatterns() []ArchitecturalPattern {
	var patterns []ArchitecturalPattern
	patterns = append(patterns, pd.detectGodObjects()...)
	patterns = append(patterns, pd.detectExcessiveParameters()...)
	patterns = append(patterns, pd.detectTooManyReturnValues()...)
	patterns = append(patterns, pd.detectInterfaceSegregation()...)
	return patterns
}

// detectGodObjects finds large structs that do too much (god objects)
func (pd *PatternDetector) detectGodObjects() []ArchitecturalPattern {
	var patterns []ArchitecturalPattern

	for _, decl := range pd.file.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
			for _, spec := range gd.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					if st, ok := ts.Type.(*ast.StructType); ok {
						fieldCount := len(st.Fields.List)
						if fieldCount > 10 {
							pattern := ArchitecturalPattern{
								Name:        "God Object",
								Type:        "smell",
								Severity:    "medium",
								Description: fmt.Sprintf("Struct %s has %d fields (>10); consider breaking into smaller types", ts.Name.Name, fieldCount),
								Affected:    []string{ts.Name.Name},
								Suggestion:  "Extract fields into logical sub-types or group by responsibility",
							}
							patterns = append(patterns, pattern)
						}
					}
				}
			}
		}
	}

	return patterns
}

// detectExcessiveParameters finds functions with too many parameters
func (pd *PatternDetector) detectExcessiveParameters() []ArchitecturalPattern {
	return pd.detectCountSmell(func(fn *ast.FuncDecl) (int, string, string) {
		count := 0
		if fn.Type.Params != nil {
			count = len(fn.Type.Params.List)
		}
		if count > 5 {
			return count, "Excessive Parameters", "Create a parameter struct to group related arguments"
		}
		return 0, "", ""
	})
}

// detectTooManyReturnValues finds functions returning too many values
func (pd *PatternDetector) detectTooManyReturnValues() []ArchitecturalPattern {
	return pd.detectCountSmell(func(fn *ast.FuncDecl) (int, string, string) {
		count := 0
		if fn.Type.Results != nil {
			count = len(fn.Type.Results.List)
		}
		if count > 3 {
			return count, "Excessive Return Values", "Create a result struct to group return values"
		}
		return 0, "", ""
	})
}

// detectCountSmell is a helper for detecting count-based smells
func (pd *PatternDetector) detectCountSmell(checker func(*ast.FuncDecl) (int, string, string)) []ArchitecturalPattern {
	var patterns []ArchitecturalPattern
	for _, decl := range pd.file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			count, name, suggestion := checker(fn)
			if count > 0 {
				pattern := ArchitecturalPattern{
					Name:        name,
					Type:        "smell",
					Severity:    "low",
					Description: fmt.Sprintf("Function %s has %d items (threshold exceeded); %s", fn.Name.Name, count, suggestion),
					Affected:    []string{fn.Name.Name},
					Suggestion:  suggestion,
				}
				patterns = append(patterns, pattern)
			}
		}
	}
	return patterns
}

// detectInterfaceSegregation finds large interfaces that might need splitting
func (pd *PatternDetector) detectInterfaceSegregation() []ArchitecturalPattern {
	var patterns []ArchitecturalPattern

	for _, decl := range pd.file.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
			for _, spec := range gd.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					if it, ok := ts.Type.(*ast.InterfaceType); ok {
						methodCount := 0
						if it.Methods != nil {
							methodCount = len(it.Methods.List)
						}

						if methodCount > 5 {
							pattern := ArchitecturalPattern{
								Name:        "Fat Interface",
								Type:        "smell",
								Severity:    "medium",
								Description: fmt.Sprintf("Interface %s has %d methods (>5); consider interface segregation", ts.Name.Name, methodCount),
								Affected:    []string{ts.Name.Name},
								Suggestion:  "Split into smaller, focused interfaces following Single Responsibility Principle",
							}
							patterns = append(patterns, pattern)
						}
					}
				}
			}
		}
	}

	return patterns
}

// DetectCircularDependencies checks for potential circular imports within package
func (pd *PatternDetector) DetectCircularDependencies() []ArchitecturalPattern {
	// This would need cross-file analysis, placeholder for now
	return []ArchitecturalPattern{}
}

// SuggestRefactorings converts detected patterns to refactoring suggestions
func (pd *PatternDetector) SuggestRefactorings() []SuggestedPlan {
	patterns := pd.DetectPatterns()
	var suggestions []SuggestedPlan

	for _, pattern := range patterns {
		suggestion := SuggestedPlan{
			Name:        fmt.Sprintf("Fix: %s", pattern.Name),
			Description: pattern.Description,
			Rationale:   pattern.Suggestion,
			Complexity:  "medium",
			SafetyRisk:  "medium",
			Operations: []SuggestedOp{
				{
					Type:        "refactor_architecture",
					Description: pattern.Suggestion,
					Priority:    priorityFromSeverity(pattern.Severity),
				},
			},
		}
		suggestions = append(suggestions, suggestion)
	}

	return suggestions
}

// priorityFromSeverity converts severity to priority score
func priorityFromSeverity(severity string) int {
	switch severity {
	case "high":
		return 9
	case "medium":
		return 6
	case "low":
		return 3
	default:
		return 5
	}
}

// Summary returns a string summary of detected patterns
func (ap *ArchitecturalPattern) Summary() string {
	return fmt.Sprintf("[%s] %s: %s", strings.ToUpper(ap.Type), ap.Name, ap.Description)
}
