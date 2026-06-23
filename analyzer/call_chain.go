package analyzer

import "fmt"

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

// FindCallChain finds all call paths from a starting function to a target
func (ca *CallAnalyzer) FindCallChain(startName, startReceiver, targetName, targetReceiver string, maxDepth int) (*CallChain, error) {
	if err := ca.symbolAnalyzer.Parse(); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
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
