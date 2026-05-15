package analyzer

import (
	"fmt"
	"go/ast"
	"os"
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

// FindCallChain finds all call paths from a starting function to a target
func (ca *CallAnalyzer) FindCallChain(startName, startReceiver, targetName, targetReceiver string, maxDepth int) (*CallChain, error) {
	if err := ca.symbolAnalyzer.Parse(); err != nil {
		return nil, err
	}
	ca.symbolAnalyzer.collectDefinitions()
	ca.buildCallGraph("", "") // Build complete call graph

	if maxDepth <= 0 {
		maxDepth = 10 // Default max depth
	}

	chain := &CallChain{
		Start:      startName,
		MaxDepth:   maxDepth,
		Confidence: 0.85,
	}

	// Find all chains from start to target
	ca.visitedChains = make(map[string]bool)
	chains := ca.findChainPaths(startName, startReceiver, targetName, targetReceiver, maxDepth, []CallSite{})
	chain.Chains = chains

	// Check for cycles
	chain.IsCircular = ca.hasCycle(startName, startReceiver)

	return chain, nil
}

// findChainPaths recursively finds all paths from start to target
func (ca *CallAnalyzer) findChainPaths(currentName, currentReceiver, targetName, targetReceiver string, depth int, currentPath []CallSite) [][]CallSite {
	var allChains [][]CallSite

	if depth <= 0 {
		return allChains
	}

	// Get callers of current function
	key := ca.buildCallKey(currentName, currentReceiver)
	callers := ca.callGraph[key]

	for _, caller := range callers {
		newPath := append(currentPath, caller)

		// Check if we found the target
		if ca.isTargetCall(caller, targetName, targetReceiver) {
			allChains = append(allChains, newPath)
		}

		// Continue searching (if not visiting same node)
		visitKey := caller.CallerName + "." + caller.CallerReceiver
		if !ca.visitedChains[visitKey] {
			ca.visitedChains[visitKey] = true
			subChains := ca.findChainPaths(caller.CallerName, caller.CallerReceiver, targetName, targetReceiver, depth-1, newPath)
			allChains = append(allChains, subChains...)
			ca.visitedChains[visitKey] = false
		}
	}

	return allChains
}

// isTargetCall checks if a call site represents a call to the target
func (ca *CallAnalyzer) isTargetCall(call CallSite, targetName, targetReceiver string) bool {
	if call.CallerName != targetName {
		return false
	}

	if targetReceiver == "" {
		return true
	}

	// Match with receiver (handle pointer variations)
	if call.CallerReceiver == targetReceiver {
		return true
	}

	if call.CallerReceiver == "*"+targetReceiver || "*"+call.CallerReceiver == targetReceiver {
		return true
	}

	return false
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

// buildCallGraph builds a complete call graph by analyzing all symbol uses
func (ca *CallAnalyzer) buildCallGraph(targetName, targetReceiver string) {
	for file, fileAST := range ca.symbolAnalyzer.fileASTs {
		// Get current function/method context as we walk
		ca.walkFileForCalls(file, fileAST)
	}

	// If searching for a method by name only, also collect all method calls
	// regardless of receiver type (since we can't determine types from AST alone)
	if targetName != "" && targetReceiver == "" {
		// Consolidate all method calls with this name
		methodCallsToAdd := make(map[string][]CallSite)
		for key, calls := range ca.callGraph {
			// Check if this is a method call to our target function
			parts := strings.Split(key, ".")
			if len(parts) == 2 && parts[1] == targetName {
				// This is a method call with name == targetName
				// Add it to the simple name key too
				if methodCallsToAdd[targetName] == nil {
					methodCallsToAdd[targetName] = []CallSite{}
				}
				methodCallsToAdd[targetName] = append(methodCallsToAdd[targetName], calls...)
			}
		}

		// Merge the consolidated calls
		for name, calls := range methodCallsToAdd {
			ca.callGraph[name] = append(ca.callGraph[name], calls...)
		}
	}
}

// walkFileForCalls walks a file's AST to find all function calls
func (ca *CallAnalyzer) walkFileForCalls(file string, fileAST *ast.File) {
	// Walk all declarations
	for _, decl := range fileAST.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			// This is a function or method
			funcName := d.Name.Name
			funcReceiver := ""

			if d.Recv != nil && len(d.Recv.List) > 0 {
				funcReceiver = ca.typeExprToString(d.Recv.List[0].Type)
			}

			// Walk the function body for calls
			if d.Body != nil {
				ca.walkBlockForCalls(d.Body, file, funcName, funcReceiver)
			}
		}
	}
}

// walkBlockForCalls walks an AST block looking for function calls
func (ca *CallAnalyzer) walkBlockForCalls(block ast.Node, file, callerName, callerReceiver string) {
	if block == nil {
		return
	}

	ast.Inspect(block, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			ca.analyzeCall(node, file, callerName, callerReceiver)
		}
		return true
	})
}

// analyzeCall extracts information from a call expression
func (ca *CallAnalyzer) analyzeCall(call *ast.CallExpr, file, callerName, callerReceiver string) {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		// Direct function call: functionName()
		callSite := CallSite{
			File:           file,
			Line:           ca.symbolAnalyzer.fset.Position(fn.Pos()).Line,
			Column:         ca.symbolAnalyzer.fset.Position(fn.Pos()).Column,
			CallerName:     callerName,
			CallerReceiver: callerReceiver,
			Snippet:        ca.getCodeSnippet(file, ca.symbolAnalyzer.fset.Position(fn.Pos()).Line),
			IsIndirect:     false,
		}

		key := ca.buildCallKey(fn.Name, "")
		ca.callGraph[key] = append(ca.callGraph[key], callSite)

	case *ast.SelectorExpr:
		// Method call: receiver.Method()
		receiverType := ca.typeExprToString(fn.X)
		callSite := CallSite{
			File:           file,
			Line:           ca.symbolAnalyzer.fset.Position(fn.Sel.Pos()).Line,
			Column:         ca.symbolAnalyzer.fset.Position(fn.Sel.Pos()).Column,
			CallerName:     callerName,
			CallerReceiver: callerReceiver,
			Snippet:        ca.getCodeSnippet(file, ca.symbolAnalyzer.fset.Position(fn.Sel.Pos()).Line),
			IsIndirect:     false,
		}

		key := ca.buildCallKey(fn.Sel.Name, receiverType)
		ca.callGraph[key] = append(ca.callGraph[key], callSite)

	case *ast.IndexExpr:
		// Function pointer call: funcPtrArray[0]()
		// Mark as indirect
		callSite := CallSite{
			File:           file,
			Line:           ca.symbolAnalyzer.fset.Position(fn.Pos()).Line,
			Column:         ca.symbolAnalyzer.fset.Position(fn.Pos()).Column,
			CallerName:     callerName,
			CallerReceiver: callerReceiver,
			Snippet:        ca.getCodeSnippet(file, ca.symbolAnalyzer.fset.Position(fn.Pos()).Line),
			IsIndirect:     true,
			IndirectType:   "function_pointer",
		}

		// We don't know which function is called, so record but mark uncertain
		ca.recordIndirectCall(callSite)
	}
}

// recordIndirectCall records a call we can't statically determine
func (ca *CallAnalyzer) recordIndirectCall(callSite CallSite) {
	// For now, just track that we found an indirect call
	// Full indirect call resolution would require more sophisticated analysis
}

// hasCycle detects if there's a cycle in the call graph from a starting point
func (ca *CallAnalyzer) hasCycle(startName, startReceiver string) bool {
	visited := make(map[string]bool)
	visiting := make(map[string]bool)

	return ca.hasCycleDFS(startName, startReceiver, visited, visiting)
}

// hasCycleDFS performs DFS to detect cycles
func (ca *CallAnalyzer) hasCycleDFS(name, receiver string, visited, visiting map[string]bool) bool {
	key := ca.buildCallKey(name, receiver)

	if visiting[key] {
		return true // Found a cycle
	}

	if visited[key] {
		return false // Already checked
	}

	visiting[key] = true

	// Check all callers of this function
	callers := ca.callGraph[key]
	for _, caller := range callers {
		if ca.hasCycleDFS(caller.CallerName, caller.CallerReceiver, visited, visiting) {
			return true
		}
	}

	visiting[key] = false
	visited[key] = true

	return false
}

// buildCallKey creates a lookup key for call graph
func (ca *CallAnalyzer) buildCallKey(name, receiver string) string {
	if receiver != "" {
		return receiver + "." + name
	}
	return name
}

// typeExprToString converts a type expression to string
func (ca *CallAnalyzer) typeExprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + ca.typeExprToString(e.X)
	case *ast.SelectorExpr:
		return ca.typeExprToString(e.X) + "." + e.Sel.Name
	default:
		return ""
	}
}

// getCodeSnippet extracts a code snippet from a file
func (ca *CallAnalyzer) getCodeSnippet(file string, line int) string {
	content, err := os.ReadFile(file)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(content), "\n")
	if line > 0 && line <= len(lines) {
		return strings.TrimSpace(lines[line-1])
	}

	return ""
}

// GetCallerHierarchy returns a hierarchical view of callers
type CallerHierarchy struct {
	FunctionName string
	Receiver     string
	File         string
	Callers      []CallerHierarchy
	CallCount    int
}

// BuildCallerHierarchy builds a tree of callers for visualization
func (ca *CallAnalyzer) BuildCallerHierarchy(name, receiver string, maxDepth int) (*CallerHierarchy, error) {
	if err := ca.symbolAnalyzer.Parse(); err != nil {
		return nil, err
	}
	ca.symbolAnalyzer.collectDefinitions()
	ca.buildCallGraph("", "")

	query := SymbolQuery{Name: name, Receiver: receiver}
	def, err := ca.symbolAnalyzer.FindSymbolDefinition(query)
	if err != nil {
		return nil, err
	}

	visited := make(map[string]bool)
	hierarchy := ca.buildHierarchyRecursive(name, receiver, def.File, maxDepth, visited)

	return hierarchy, nil
}

// buildHierarchyRecursive recursively builds caller hierarchy
func (ca *CallAnalyzer) buildHierarchyRecursive(name, receiver, file string, depth int, visited map[string]bool) *CallerHierarchy {
	key := ca.buildCallKey(name, receiver)

	if depth <= 0 || visited[key] {
		return &CallerHierarchy{
			FunctionName: name,
			Receiver:     receiver,
			File:         file,
		}
	}

	visited[key] = true

	hierarchy := &CallerHierarchy{
		FunctionName: name,
		Receiver:     receiver,
		File:         file,
		Callers:      []CallerHierarchy{},
	}

	// Add direct callers
	callers := ca.callGraph[key]
	hierarchy.CallCount = len(callers)

	// Get unique callers to avoid duplicates in hierarchy
	uniqueCallers := make(map[string]CallSite)
	for _, caller := range callers {
		callerKey := caller.CallerName + ":" + caller.CallerReceiver
		if _, exists := uniqueCallers[callerKey]; !exists {
			uniqueCallers[callerKey] = caller
		}
	}

	for _, caller := range uniqueCallers {
		childHierarchy := ca.buildHierarchyRecursive(caller.CallerName, caller.CallerReceiver, caller.File, depth-1, visited)
		hierarchy.Callers = append(hierarchy.Callers, *childHierarchy)
	}

	return hierarchy
}
