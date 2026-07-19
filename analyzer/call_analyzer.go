package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// CallSite represents a location where a function/method is called
type CallSite struct {
	File           string `json:"file"`
	Line           int    `json:"line"`
	Column         int    `json:"column"`
	CallerName     string `json:"callerName"`
	CallerReceiver string `json:"callerReceiver,omitempty"` // For method callers
	Snippet        string `json:"snippet"`
	IsIndirect     bool   `json:"isIndirect"`             // Called via interface or function pointer
	IndirectType   string `json:"indirectType,omitempty"` // Interface or func type
}

// CallerAnalysis contains all information about callers of a symbol
type CallerAnalysis struct {
	TargetName      string     `json:"targetName"`
	TargetReceiver  string     `json:"targetReceiver,omitempty"`
	TargetFile      string     `json:"targetFile"`
	TargetLine      int        `json:"targetLine"`
	DirectCallers   []CallSite `json:"directCallers"`
	IndirectCallers []CallSite `json:"indirectCallers"`
	TestCallers     []CallSite `json:"testCallers"`
	TotalCallCount  int        `json:"totalCallCount"`
	IsExported      bool       `json:"isExported"`
	Confidence      float64    `json:"confidence"` // 0-1, confidence in analysis
}

// CallAnalyzer analyzes function calls
type CallAnalyzer struct {
	symbolAnalyzer *UseAnalyzer
	files          []string
	callGraph      map[string][]CallSite // Maps function name to all callers
	definitions    map[string]*SymbolDefinition
	snippetLines   map[string][]string // Cache of file -> lines for snippet extraction
}

// NewCallAnalyzer creates a new call analyzer
func NewCallAnalyzer(files []string) *CallAnalyzer {
	return &CallAnalyzer{
		symbolAnalyzer: NewUseAnalyzer(files),
		files:          files,
		callGraph:      make(map[string][]CallSite),
		definitions:    make(map[string]*SymbolDefinition),
		snippetLines:   make(map[string][]string),
	}
}

// SeedASTs reuses pre-parsed ASTs for the underlying symbol analyzer, so the
// call graph is built without re-reading or re-parsing files. See
// UseAnalyzer.SeedASTs.
func (ca *CallAnalyzer) SeedASTs(fset *token.FileSet, asts map[string]*ast.File) {
	ca.symbolAnalyzer.SeedASTs(fset, asts)
}

// FindCallers finds all functions/methods that call a target function
func (ca *CallAnalyzer) FindCallers(targetName, targetReceiver string) (*CallerAnalysis, error) {
	// Parse all files and collect definitions
	if err := ca.symbolAnalyzer.Parse(); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	ca.symbolAnalyzer.collectDefinitions()

	// Find the target definition
	var targetDef *SymbolDefinition
	query := SymbolQuery{Name: targetName, Receiver: targetReceiver}
	var err error
	targetDef, err = ca.symbolAnalyzer.FindSymbolDefinition(query)
	if err != nil {
		return nil, fmt.Errorf("target not found: %s", targetName)
	}

	// Build call graph
	ca.buildCallGraph(targetName, targetReceiver)

	// Analyze callers
	analysis := &CallerAnalysis{
		TargetName:     targetName,
		TargetReceiver: targetReceiver,
		TargetFile:     targetDef.File,
		TargetLine:     targetDef.Line,
		IsExported:     targetDef.IsExported,
		Confidence:     0.95, // High confidence for direct calls
	}

	// Get all callers from graph
	key := ca.buildCallKey(targetName, targetReceiver)
	allCallers := ca.callGraph[key]

	// Categorize callers
	for _, caller := range allCallers {
		if strings.Contains(caller.File, "_test.go") {
			analysis.TestCallers = append(analysis.TestCallers, caller)
		} else if caller.IsIndirect {
			analysis.IndirectCallers = append(analysis.IndirectCallers, caller)
		} else {
			analysis.DirectCallers = append(analysis.DirectCallers, caller)
		}
	}

	analysis.TotalCallCount = len(allCallers)

	return analysis, nil
}
