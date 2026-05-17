package analyzer

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

// PlanSuggester generates refactoring plan recommendations
type PlanSuggester struct {
	filePath string
	fset     *token.FileSet
	File     *ast.File
}

// SuggestedPlan represents a refactoring plan recommendation
type SuggestedPlan struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Operations  []SuggestedOp `json:"operations"`
	Rationale   string        `json:"rationale"`
	Complexity  string        `json:"complexity"` // low, medium, high
	SafetyRisk  string        `json:"safetyRisk"`
}

// SuggestedOp represents a single suggested operation
type SuggestedOp struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Target      map[string]interface{} `json:"target"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
	Priority    int                    `json:"priority"` // 1-10, 10 being highest
}

// NewPlanSuggester creates a suggester for a file
func NewPlanSuggester(filePath string) (*PlanSuggester, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, content, parser.AllErrors)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	return &PlanSuggester{
		filePath: filePath,
		fset:     fset,
		File:     file,
	}, nil
}

// SuggestExtractions suggests method extraction opportunities
func (ps *PlanSuggester) SuggestExtractions() []SuggestedPlan {
	var plans []SuggestedPlan

	// Find functions with high complexity
	for _, decl := range ps.File.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			complexity := ps.analyzeComplexity(fn)

			// Recommend extraction if complexity is moderate-to-high (5-15)
			if complexity >= 5 && complexity <= 15 {
				plan := SuggestedPlan{
					Name:        fmt.Sprintf("Extract from %s", fn.Name.Name),
					Description: fmt.Sprintf("Function %s has complexity %d; consider extracting helper methods", fn.Name.Name, complexity),
					Rationale:   "Reduces cognitive load and improves testability",
					Complexity:  ps.complexityLevel(complexity),
					SafetyRisk:  "low",
					Operations: []SuggestedOp{
						{
							Type:        "extract_method",
							Description: fmt.Sprintf("Extract a block from %s into a new helper function", fn.Name.Name),
							Target: map[string]interface{}{
								"functionName": fn.Name.Name,
							},
							Priority: 7,
						},
					},
				}
				plans = append(plans, plan)
			}
		}
	}

	return plans
}

// SuggestRenames suggests symbols that might benefit from renaming
func (ps *PlanSuggester) SuggestRenames() []SuggestedPlan {
	var plans []SuggestedPlan

	// Suggest renaming unexported symbols that could be exported
	for _, decl := range ps.File.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if !isExported(fn.Name.Name) && ps.shouldBeExported(fn) {
				plan := SuggestedPlan{
					Name:        fmt.Sprintf("Export %s", fn.Name.Name),
					Description: fmt.Sprintf("Consider exporting %s (capitalize first letter)", fn.Name.Name),
					Rationale:   "API clarity - exported names are more discoverable",
					Complexity:  "low",
					SafetyRisk:  "medium",
					Operations: []SuggestedOp{
						{
							Type:        "rename_declaration",
							Description: fmt.Sprintf("Rename %s to %s", fn.Name.Name, strings.ToUpper(fn.Name.Name[:1])+fn.Name.Name[1:]),
							Target: map[string]interface{}{
								"functionName": fn.Name.Name,
							},
							Parameters: map[string]interface{}{
								"newName": strings.ToUpper(fn.Name.Name[:1]) + fn.Name.Name[1:],
							},
							Priority: 5,
						},
					},
				}
				plans = append(plans, plan)
			}
		}
	}

	return plans
}

// SuggestReorganization suggests file reorganization
func (ps *PlanSuggester) SuggestReorganization() []SuggestedPlan {
	var plans []SuggestedPlan

	fileSize := ps.countLines()

	// Suggest splitting large files
	if fileSize > 500 {
		plan := SuggestedPlan{
			Name:        "Split large file",
			Description: fmt.Sprintf("File has %d lines; consider splitting by type or functionality", fileSize),
			Rationale:   "Easier maintenance and navigation",
			Complexity:  "medium",
			SafetyRisk:  "low",
			Operations: []SuggestedOp{
				{
					Type:        "split_file",
					Description: "Split file by grouping related functions",
					Target: map[string]interface{}{
						"filePath": ps.filePath,
					},
					Priority: 6,
				},
			},
		}
		plans = append(plans, plan)
	}

	return plans
}

// AllSuggestions returns all suggestions for the file
func (ps *PlanSuggester) AllSuggestions() []SuggestedPlan {
	var all []SuggestedPlan
	all = append(all, ps.SuggestExtractions()...)
	all = append(all, ps.SuggestRenames()...)
	all = append(all, ps.SuggestReorganization()...)
	return all
}

// analyzeComplexity calculates complexity score for a function
func (ps *PlanSuggester) analyzeComplexity(fn *ast.FuncDecl) int {
	score := 1
	if fn.Body != nil {
		score += len(fn.Body.List)
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			switch node.(type) {
			case *ast.IfStmt, *ast.ForStmt, *ast.SwitchStmt, *ast.SelectStmt:
				score++
			case *ast.CaseClause:
				score++
			}
			return true
		})
	}
	return score
}

// complexityLevel returns a human-readable complexity level
func (ps *PlanSuggester) complexityLevel(score int) string {
	if score < 5 {
		return "low"
	} else if score < 10 {
		return "medium"
	}
	return "high"
}

// shouldBeExported heuristically determines if a function should be exported
func (ps *PlanSuggester) shouldBeExported(fn *ast.FuncDecl) bool {
	// Heuristic: if function is called from package init or has documentation comment
	return ps.hasDocComment(fn) && !hasPrivatePrefix(fn.Name.Name)
}

// hasDocComment checks if a function has a documentation comment
func (ps *PlanSuggester) hasDocComment(fn *ast.FuncDecl) bool {
	return fn.Doc != nil && len(fn.Doc.List) > 0
}

// hasPrivatePrefix checks if a name suggests it's private
func hasPrivatePrefix(name string) bool {
	return strings.HasPrefix(name, "private") || strings.HasPrefix(name, "internal")
}

// isExported checks if a name is exported (starts with uppercase)
func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}

// countLines counts total lines in the file
func (ps *PlanSuggester) countLines() int {
	if ps.File == nil {
		return 0
	}
	f := ps.fset.File(ps.File.End())
	if f == nil {
		return 0
	}
	return f.LineCount()
}

// ToJSON exports suggestions as JSON
func (s *SuggestedPlan) ToJSON() (string, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Summary returns a string summary of suggestions
func (s *SuggestedPlan) Summary() string {
	return fmt.Sprintf("%s: %s (Complexity: %s, Risk: %s)", s.Name, s.Description, s.Complexity, s.SafetyRisk)
}
