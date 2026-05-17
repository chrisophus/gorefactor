package analyzer

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
