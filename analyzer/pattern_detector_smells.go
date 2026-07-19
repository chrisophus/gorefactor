package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
)

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

func (pd *PatternDetector) forEachTypeSpec(visit func(ts *ast.TypeSpec)) {
	for _, decl := range pd.file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			if ts, ok := spec.(*ast.TypeSpec); ok {
				visit(ts)
			}
		}
	}
}

func (pd *PatternDetector) methodCountByReceiver() map[string]int {
	counts := make(map[string]int)
	for

	// detectGodObjects finds large structs that do too much: many fields *and*
	// non-trivial method count. A Go struct with many fields and zero methods is
	// a record/DTO (idiomatic in Go — cf. http.Request, tls.Config), not a god
	// object.
	_, decl := range pd.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		counts[getReceiverTypeName(fn.Recv.List[0])]++
	}
	return counts
}

func (pd *PatternDetector) detectGodObjects() []ArchitecturalPattern {
	var patterns []ArchitecturalPattern
	classMethods := pd.methodCountByReceiver()

	pd.forEachTypeSpec(func(ts *ast.TypeSpec) {
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return
		}
		fieldCount := len(st.Fields.List)
		methodCount := classMethods[ts.Name.Name]
		if fieldCount <= 25 || methodCount == 0 {
			return
		}
		patterns = append(patterns, ArchitecturalPattern{
			Name:        "God Object",
			Type:        "smell",
			Severity:    "medium",
			Description: fmt.Sprintf("Struct %s has %d fields (>25) and %d methods; consider breaking into smaller types", ts.Name.Name, fieldCount, methodCount),
			Affected:    []string{ts.Name.Name},
			Suggestion:  "Extract fields into logical sub-types or group by responsibility",
		})
	})

	return patterns

}

// detectExcessiveParameters finds functions with too many parameters
func (pd *PatternDetector) detectExcessiveParameters() []ArchitecturalPattern {
	return pd.detectCountSmell(func(fn *ast.FuncDecl) (int, string, string) {
		count := 0
		if fn.Type.Params != nil {
			count = len(fn.Type.Params.List)
		}
		if count > 7 {
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

	pd.forEachTypeSpec(func(ts *ast.TypeSpec) {
		it, ok := ts.Type.(*ast.InterfaceType)
		if !ok {
			return
		}
		methodCount := 0
		if it.Methods != nil {
			methodCount = len(it.Methods.List)
		}
		if methodCount <= 5 {
			return
		}
		patterns = append(patterns, ArchitecturalPattern{
			Name:        "Fat Interface",
			Type:        "smell",
			Severity:    "medium",
			Description: fmt.Sprintf("Interface %s has %d methods (>5); idiomatic Go declares interfaces at the consumer side — move to the package that uses %s and narrow to just the methods that package needs", ts.Name.Name, methodCount, ts.Name.Name),
			Affected:    []string{ts.Name.Name},
			Suggestion:  "Relocate to consumer side and narrow, rather than splitting at the declaration site",
		})
	})

	return patterns

}

// detectLargeClass finds structs with substantial behavior — many methods, or
// many total members AND at least one method. Pure-data structs (no methods)
// are records and shouldn't trip this rule, regardless of field count.
func (pd *PatternDetector) detectLargeClass() []ArchitecturalPattern {
	var patterns []ArchitecturalPattern
	classMethods := pd.methodCountByReceiver()

	pd.forEachTypeSpec(func(ts *ast.TypeSpec) {
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return
		}
		fieldCount := 0
		if st.Fields != nil {
			fieldCount = len(st.Fields.List)
		}
		methodCount := classMethods[ts.Name.Name]
		totalMembers := fieldCount + methodCount

		if methodCount <= 20 && (totalMembers <= 30 || methodCount == 0) {
			return
		}
		patterns = append(patterns, ArchitecturalPattern{
			Name:        "Large Class",
			Type:        "smell",
			Severity:    "medium",
			Description: fmt.Sprintf("Type %s has %d fields + %d methods = %d total members; consider extraction", ts.Name.Name, fieldCount, methodCount, totalMembers),
			Affected:    []string{ts.Name.Name},
			Suggestion:  "Extract cohesive methods into a new type or extract related fields into a sub-type",
		})
	})

	return patterns

}

// detectSwitchStatements finds type-switch dispatch repeated across many
// functions in the same file. Type switches over heterogeneous AST/visitor
// types are idiomatic Go, so thresholds are kept high — only flag when a
// single function does >1 type switch *and* at least 3 functions in the file
// do type dispatch.
func (pd *PatternDetector) detectSwitchStatements() []ArchitecturalPattern {
	var patterns []ArchitecturalPattern
	switchCount := make(map[string]int)

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
			}
		}
	}

	if len(switchCount) < 3 {
		return patterns
	}
	for funcName, count := range switchCount {
		if count <= 1 {
			continue
		}
		severity := "low"
		if count > 2 {
			severity = "medium"
		}
		pattern := ArchitecturalPattern{
			Name:        "Type Switches",
			Type:        "smell",
			Severity:    severity,
			Description: fmt.Sprintf("Function %s contains %d type switches; %d functions in this file do type dispatch", funcName, count, len(switchCount)),
			Affected:    []string{funcName},
			Suggestion:  "If types are user-owned (not go/ast etc), consider polymorphism. For closed AST-like type sets, type switches are idiomatic.",
		}
		patterns = append(patterns, pattern)
	}

	return patterns
}

// This would need cross-file analysis, placeholder for now
