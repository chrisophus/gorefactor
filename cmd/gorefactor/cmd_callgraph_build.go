package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
)

// buildCallIndex parses every file once and records call edges between
// declared functions. Selector calls (x.Foo()) are matched by method name;
// ident calls (Foo()) are matched against plain functions.
func buildCallIndex(files []string) (*cgIndex, error) {
	idx := &cgIndex{
		defs:    map[string]*cgDef{},
		callees: map[string][]*cgDef{},
		callers: map[string][]*cgDef{},
	}
	fset := token.NewFileSet()
	type rawCall struct {
		caller   *cgDef
		name     string
		selector bool
	}
	var calls []rawCall

	for _, file := range files {
		astFile, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			continue // skip unparseable files; sensors are best-effort
		}
		for _, decl := range astFile.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			def := &cgDef{
				name:     fn.Name.Name,
				receiver: cgReceiver(fn),
				file:     file,
				line:     fset.Position(fn.Pos()).Line,
			}
			if _, exists := idx.defs[def.key()]; !exists {
				idx.defs[def.key()] = def
			}
			caller := idx.defs[def.key()]
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch f := call.Fun.(type) {
				case *ast.Ident:
					calls = append(calls, rawCall{caller, f.Name, false})
				case *ast.SelectorExpr:
					calls = append(calls, rawCall{caller, f.Sel.Name, true})
				}
				return true
			})
		}
	}

	// Resolve raw calls against the definition index.
	seen := map[string]bool{} // "callerKey->calleeKey" dedupe
	for _, c := range calls {
		for _, callee := range resolveCallee(idx, c.name, c.selector) {
			edge := c.caller.key() + "->" + callee.key()
			if seen[edge] {
				continue
			}
			seen[edge] = true
			idx.callees[c.caller.key()] = append(idx.callees[c.caller.key()], callee)
			idx.callers[callee.key()] = append(idx.callers[callee.key()], c.caller)
		}
	}
	for _, m := range []map[string][]*cgDef{idx.callees, idx.callers} {
		for k := range m {
			sort.Slice(m[k], func(i, j int) bool { return m[k][i].key() < m[k][j].key() })
		}
	}
	return idx, nil
}
