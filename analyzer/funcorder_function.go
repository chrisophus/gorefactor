package analyzer

import (
	"go/ast"
	"go/token"
)

// funcorderFunctionIssue reports the funcorder-function violation, if any:
// among the file's loose (non-method, non-init, non-constructor) top-level
// functions, no unexported function may be declared before an exported one.
func funcorderFunctionIssue(f *ast.File, fset *token.FileSet, filename string, groups []*funcorderGroup) (FuncorderIssue, bool) {
	loose := funcorderLooseFuncIndices(f, funcorderCtorIdxSet(groups))
	if len(loose) < 2 {
		return FuncorderIssue{}, false
	}
	// Find the earliest loose function that is unexported and precedes a
	// later exported one.
	maxExported := -1
	for _, idx := range loose {
		fn := f.Decls[idx].(*ast.FuncDecl)
		if fn.Name.IsExported() && idx > maxExported {
			maxExported = idx
		}
	}
	for _, idx := range loose {
		fn := f.Decls[idx].(*ast.FuncDecl)
		if !fn.Name.IsExported() && idx < maxExported {
			pos := fset.Position(fn.Pos())
			return FuncorderIssue{
				File:     filename,
				Line:     pos.Line,
				Column:   pos.Column,
				Rule:     funcorderFunctionRuleName,
				Message:  "exported functions must be declared before unexported functions (excluding constructors and init)",
				FuncName: fn.Name.Name,
			}, true
		}
	}
	return FuncorderIssue{}, false
}

// funcorderLooseFuncIndices returns, in file order, the Decls-indices of
// top-level, receiver-less functions that are not `init()` and not a
// tracked constructor.
func funcorderLooseFuncIndices(f *ast.File, ctorIdx map[int]bool) []int {
	var out []int
	for i, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			continue
		}
		if fn.Name.Name == "init" {
			continue
		}
		if ctorIdx[i] {
			continue
		}
		out = append(out, i)
	}
	return out
}

// funcorderCtorIdxSet collects the Decls-indices of every constructor
// tracked by any struct group, so loose-function detection can exclude them
// (they're governed by funcorder-constructor instead).
func funcorderCtorIdxSet(groups []*funcorderGroup) map[int]bool {
	set := make(map[int]bool)
	for _, g := range groups {
		for _, idx := range g.ctorIdx {
			set[idx] = true
		}
	}
	return set
}

// funcorderStablePartitionExported stable-partitions declIdx (Decls-indices
// of top-level FuncDecls) into exported functions followed by unexported
// functions, preserving each subgroup's relative order.
func funcorderStablePartitionExported(f *ast.File, declIdx []int) []int {
	out := make([]int, 0, len(declIdx))
	var unexported []int
	for _, idx := range declIdx {
		fn := f.Decls[idx].(*ast.FuncDecl)
		if fn.Name.IsExported() {
			out = append(out, idx)
		} else {
			unexported = append(unexported, idx)
		}
	}
	return append(out, unexported...)
}
