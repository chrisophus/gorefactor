package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
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
	patterns = append(patterns, pd.detectLargeClass()...)
	patterns = append(patterns, pd.detectDataClumps()...)
	patterns = append(patterns, pd.detectSwitchStatements()...)
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

// detectLargeClass finds structs with too many methods (>20) or fields (>15)
func (pd *PatternDetector) detectLargeClass() []ArchitecturalPattern {
	var patterns []ArchitecturalPattern
	classMethods := make(map[string]int)

	// Count methods per receiver type
	for _, decl := range pd.file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv != nil {
			if len(fn.Recv.List) > 0 {
				recvType := getReceiverTypeName(fn.Recv.List[0])
				classMethods[recvType]++
			}
		}
	}

	// Check structs: field count + method count
	for _, decl := range pd.file.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
			for _, spec := range gd.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					if st, ok := ts.Type.(*ast.StructType); ok {
						fieldCount := 0
						if st.Fields != nil {
							fieldCount = len(st.Fields.List)
						}
						methodCount := classMethods[ts.Name.Name]
						totalMembers := fieldCount + methodCount

						if fieldCount > 15 || methodCount > 20 || totalMembers > 30 {
							severity := "low"
							if methodCount > 20 || totalMembers > 30 {
								severity = "medium"
							}
							pattern := ArchitecturalPattern{
								Name:        "Large Class",
								Type:        "smell",
								Severity:    severity,
								Description: fmt.Sprintf("Type %s has %d fields + %d methods = %d total members; consider extraction", ts.Name.Name, fieldCount, methodCount, totalMembers),
								Affected:    []string{ts.Name.Name},
								Suggestion:  "Extract cohesive methods into a new type or extract related fields into a sub-type",
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

// detectDataClumps finds typed parameter groups (>=3 params) that recur
// across >=2 functions in the file -- a Fowler "data clump" that likely
// wants to become its own struct. The signature is keyed on (name,type)
// pairs so int/string params are never conflated, order-normalized so a
// reordered group still matches, and emitted in deterministic order so
// the output does not depend on map iteration.
func (pd *PatternDetector) detectDataClumps() []ArchitecturalPattern {
	type clump struct {
		display string   // human-readable "name type, name type"
		params  []string // affected param names (first occurrence order)
		count   int
	}
	clumps := make(map[string]*clump)

	for _, decl := range pd.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Type.Params == nil {
			continue
		}
		var pairs, names []string
		for _, field := range fn.Type.Params.List {
			t := paramTypeString(field.Type)
			for _, name := range field.Names {
				pairs = append(pairs, name.Name+" "+t)
				names = append(names, name.Name)
			}
		}
		if len(pairs) < 3 {
			continue
		}
		key := append([]string(nil), pairs...)
		sort.Strings(key)
		sig := strings.Join(key, ", ")
		c := clumps[sig]
		if c == nil {
			c = &clump{display: strings.Join(pairs, ", "), params: names}
			clumps[sig] = c
		}
		c.count++
	}

	var sigs []string
	for sig, c := range clumps {
		if c.count >= 2 {
			sigs = append(sigs, sig)
		}
	}
	sort.Strings(sigs)

	var patterns []ArchitecturalPattern
	for _, sig := range sigs {
		c := clumps[sig]
		patterns = append(patterns, ArchitecturalPattern{
			Name:        "Data Clumps",
			Type:        "smell",
			Severity:    "low",
			Description: fmt.Sprintf("Parameter group [%s] appears in %d functions; consider creating a struct", c.display, c.count),
			Affected:    c.params,
			Suggestion:  "Create a struct grouping these related fields, use it in function signatures",
		})
	}
	return patterns
}

// detectSwitchStatements finds switch statements on type fields
func (pd *PatternDetector) detectSwitchStatements() []ArchitecturalPattern {
	var patterns []ArchitecturalPattern
	switchCount := make(map[string]int)
	switchFuncs := make(map[string][]string)

	for _, decl := range pd.file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			count := countSwitchStatements(fn.Body)
			if count > 0 {
				functionName := fn.Name.Name
				if fn.Recv != nil && len(fn.Recv.List) > 0 {
					recvType := getReceiverTypeName(fn.Recv.List[0])
					functionName = recvType + ":" + functionName
				}
				switchCount[functionName] = count
				switchFuncs[functionName] = []string{functionName}
			}
		}
	}

	// Flag if switch statements are scattered (multiple functions with switches)
	if len(switchCount) > 1 {
		for funcName, count := range switchCount {
			if count > 0 {
				severity := "low"
				if count > 2 {
					severity = "medium"
				}
				pattern := ArchitecturalPattern{
					Name:        "Switch Statements",
					Type:        "smell",
					Severity:    severity,
					Description: fmt.Sprintf("Function %s contains %d switch statement(s) on type; pattern is scattered across codebase", funcName, count),
					Affected:    []string{funcName},
					Suggestion:  "Consider replacing scattered switches with polymorphism (strategy pattern or interface-based dispatch)",
				}
				patterns = append(patterns, pattern)
			}
		}
	}

	return patterns
}

// Helper functions

// getReceiverTypeName extracts the receiver type name from a field
func getReceiverTypeName(field *ast.Field) string {
	if star, ok := field.Type.(*ast.StarExpr); ok {
		if ident, ok := star.X.(*ast.Ident); ok {
			return ident.Name
		}
	}
	if ident, ok := field.Type.(*ast.Ident); ok {
		return ident.Name
	}
	return "Unknown"
}

func paramTypeString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + paramTypeString(e.X)
	case *ast.SelectorExpr:
		return paramTypeString(e.X) + "." + e.Sel.Name
	case *ast.ArrayType:
		return "[]" + paramTypeString(e.Elt)
	case *ast.MapType:
		return "map[" + paramTypeString(e.Key) + "]" + paramTypeString(e.Value)
	case *ast.Ellipsis:
		return "..." + paramTypeString(e.Elt)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func"
	default:
		return "expr"
	}
}

// countSwitchStatements recursively counts switch statements in a block
func countSwitchStatements(block *ast.BlockStmt) int {
	if block == nil {
		return 0
	}

	count := 0
	for _, stmt := range block.List {
		count += countSwitchInStmt(stmt)
	}
	return count
}

// countSwitchInStmt recursively counts switch statements
func countSwitchInStmt(stmt ast.Stmt) int {
	count := 0
	switch s := stmt.(type) {
	case *ast.SwitchStmt:
		count++
		if s.Body != nil {
			for _, s2 := range s.Body.List {
				count += countSwitchInStmt(s2)
			}
		}
	case *ast.TypeSwitchStmt:
		count++
		if s.Body != nil {
			for _, s2 := range s.Body.List {
				count += countSwitchInStmt(s2)
			}
		}
	case *ast.IfStmt:
		if s.Body != nil {
			count += countSwitchStatements(s.Body)
		}
		if s.Else != nil {
			count += countSwitchInStmt(s.Else)
		}
	case *ast.ForStmt:
		if s.Body != nil {
			count += countSwitchStatements(s.Body)
		}
	case *ast.RangeStmt:
		if s.Body != nil {
			count += countSwitchStatements(s.Body)
		}
	case *ast.BlockStmt:
		count += countSwitchStatements(s)
	}
	return count
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
