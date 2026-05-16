package analyzer

import (
	"go/ast"
	"go/token"
)

func applyAssignStmt(node *ast.AssignStmt, info *BlockInfo) {
	for _, lhs := range node.Lhs {
		if ident, ok := lhs.(*ast.Ident); ok {
			info.WriteVars = append(info.WriteVars, ident.Name)
			info.Assignments = append(info.Assignments, ident.Name)
		}
	}
	info.StatementCount++
}

func applyBinaryExpr(node *ast.BinaryExpr, info *BlockInfo) {
	if node.Op == token.LAND || node.Op == token.LOR {
		info.LogicalOperators++
		info.Complexity++
	}
}

func applyCallExpr(node *ast.CallExpr, info *BlockInfo) {
	if ident, ok := node.Fun.(*ast.Ident); ok {
		info.FunctionCalls = append(info.FunctionCalls, ident.Name)
	}
	info.StatementCount++
}

func applyRangeNode(n ast.Node, info *BlockInfo) {
	switch node := n.(type) {
	case *ast.BlockStmt:
		analyzeBlock(node, info)
	case *ast.AssignStmt:
		applyAssignStmt(node, info)
	case *ast.Ident:
		if node.Obj != nil {
			info.ReadVars = append(info.ReadVars, node.Name)
		}
	case *ast.IfStmt:
		info.ControlStructures++
		info.Complexity++
		info.StatementCount++
	case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt:
		info.ControlStructures++
		info.Complexity++
		info.StatementCount++
	case *ast.BinaryExpr:
		applyBinaryExpr(node, info)
	case *ast.CallExpr:
		applyCallExpr(node, info)
	case *ast.ReturnStmt:
		info.ReturnCount++
		info.StatementCount++
	case *ast.ExprStmt, *ast.DeclStmt:
		info.StatementCount++
	}
}

func recordIdent(node *ast.Ident, readVars map[string]bool, variableScopes map[string][]int) {
	if node.Obj != nil {
		readVars[node.Name] = true
		if _, exists := variableScopes[node.Name]; !exists {
			variableScopes[node.Name] = []int{int(node.Pos())}
		}
		variableScopes[node.Name] = append(variableScopes[node.Name], int(node.End()))
	}
}

func recordAssignment(node *ast.AssignStmt, info *BlockInfo, writeVars, assignments map[string]bool) {
	for _, lhs := range node.Lhs {
		if ident, ok := lhs.(*ast.Ident); ok {
			writeVars[ident.Name] = true
			assignments[ident.Name] = true
		}
	}
	info.StatementCount++
}

func recordIfStmt(node *ast.IfStmt, info *BlockInfo, currentNesting, maxNesting *int) {
	info.ControlStructures++
	info.Complexity++
	info.StatementCount++
	*currentNesting++
	if *currentNesting > *maxNesting {
		*maxNesting = *currentNesting
	}
	if len(node.Body.List) > 0 {
		if _, ok := node.Body.List[0].(*ast.ReturnStmt); ok {
			info.ErrorHandlingPaths++
		}
	}
}

func recordLoopStmt(info *BlockInfo, currentNesting, maxNesting *int) {
	info.ControlStructures++
	info.Complexity++
	info.StatementCount++
	*currentNesting++
	if *currentNesting > *maxNesting {
		*maxNesting = *currentNesting
	}
}

func buildVariableInfo(readVars, writeVars, assignments map[string]bool, info *BlockInfo) {
	for v := range readVars {
		info.ReadVars = append(info.ReadVars, v)
	}
	for v := range writeVars {
		info.WriteVars = append(info.WriteVars, v)
	}
	for v := range assignments {
		info.Assignments = append(info.Assignments, v)
	}
}
