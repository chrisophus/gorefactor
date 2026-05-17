package analyzer

import (
	"go/ast"
	"os"
	"strings"
)

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
