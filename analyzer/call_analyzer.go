package analyzer

import (
	"fmt"
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

// CallChain represents a chain of function calls (A calls B calls C)
type CallChain struct {
	Start      string       `json:"start"`
	Chains     [][]CallSite `json:"chains"`
	MaxDepth   int          `json:"maxDepth"`
	IsCircular bool         `json:"isCircular"`
	Confidence float64      `json:"confidence"`
}

// CallAnalyzer analyzes function calls
type CallAnalyzer struct {
	symbolAnalyzer *UseAnalyzer
	files          []string
	callGraph      map[string][]CallSite // Maps function name to all callers
	definitions    map[string]*SymbolDefinition
	visitedChains  map[string]bool // For cycle detection
}

// NewCallAnalyzer creates a new call analyzer
func NewCallAnalyzer(files []string) *CallAnalyzer {
	return &CallAnalyzer{
		symbolAnalyzer: NewUseAnalyzer(files),
		files:          files,
		callGraph:      make(map[string][]CallSite),
		definitions:    make(map[string]*SymbolDefinition),
		visitedChains:  make(map[string]bool),
	}
}

// FindCallers finds all functions/methods that call a target function
func (ca *CallAnalyzer) FindCallers(targetName, targetReceiver string) (*CallerAnalysis, error) {
	// Parse all files and collect definitions
	if err := ca.symbolAnalyzer.Parse(); err != nil {
		return nil, err
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

// IsCallableFrom checks if one function can call another
func (ca *CallAnalyzer) IsCallableFrom(callerName, callerReceiver, targetName, targetReceiver string) (bool, error) {
	if err := ca.symbolAnalyzer.Parse(); err != nil {
		return false, err
	}
	ca.symbolAnalyzer.collectDefinitions()
	ca.buildCallGraph(targetName, targetReceiver)

	key := ca.buildCallKey(targetName, targetReceiver)
	callers := ca.callGraph[key]

	for _, caller := range callers {
		if caller.CallerName == callerName {
			if callerReceiver == "" || caller.CallerReceiver == callerReceiver ||
				caller.CallerReceiver == "*"+callerReceiver ||
				"*"+caller.CallerReceiver == callerReceiver {
				return true, nil
			}
		}
	}

	return false, nil
}

// GetCallerHierarchy returns a hierarchical view of callers
type CallerHierarchy struct {
	FunctionName string
	Receiver     string
	File         string
	Callers      []CallerHierarchy
	CallCount    int
}
