package main

import (
	"bytes"
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
)

var inlineFlags = mutFlagSpec(nil)

func init() {
	registerCommand(Command{
		Name:        "inline",
		Mutates:     true,
		MCPTool:     true,
		TxnSafe:     true,
		Description: "Inline a simple function into its call sites and delete it (refuses anything complex)",
		Usage:       "inline <file> <Func> [--json] [--dry-run] [--gate]",
		MinArgs:     2,
		MaxArgs:     2,
		Flags:       inlineFlags,
		Run:         inlineCommand,
	})
}

// inlineTextEdit is a byte-range replacement within one file (in-memory).
type inlineTextEdit struct {
	start, end int
	text       string
}

func applyInlineEdits(src []byte, edits []inlineTextEdit) ([]byte, error) {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := src
	last := -1
	for _, e := range edits {
		if e.start < 0 || e.end > len(src) || e.start > e.end {
			return nil, fmt.Errorf("internal error: edit range [%d,%d) out of bounds", e.start, e.end)
		}
		if last >= 0 && e.end > last {
			return nil, fmt.Errorf("internal error: overlapping edits")
		}
		last = e.start
		var next []byte
		next = append(next, out[:e.start]...)
		next = append(next, []byte(e.text)...)
		next = append(next, out[e.end:]...)
		out = next
	}
	return out, nil
}

// inlineTemplate is the substitutable body of the function being inlined.
type inlineTemplate struct {
	exprMode   bool   // true: single `return expr`; false: statement list
	body       string // source text of the return expression or statement list
	returnExpr ast.Expr
	params     []string
	// occurrences of parameters within body, as (relative start, relative
	// end, param index), in source order.
	uses []paramUse
}

type paramUse struct {
	start, end, param int
}

// bodyRegion adapts a (Pos, End) pair to ast.Node for offset extraction.
type bodyRegion struct{ pos, end token.Pos }

func (r bodyRegion) Pos() token.Pos { return r.pos }
func (r bodyRegion) End() token.Pos { return r.end }

func regionAST(fd *ast.FuncDecl, exprMode bool) ast.Node {
	if exprMode {
		return fd.Body.List[0].(*ast.ReturnStmt).Results[0]
	}
	return fd.Body
}

// flattenParamNames returns the parameter names in order, refusing variadic
// and unnamed/blank parameters.
func flattenParamNames(fd *ast.FuncDecl, name string) ([]string, error) {
	var params []string
	if fd.Type.Params == nil {
		return params, nil
	}
	for _, f := range fd.Type.Params.List {
		if _, variadic := f.Type.(*ast.Ellipsis); variadic {
			return nil, parseErrorf("cannot inline %s: variadic functions are not supported", name)
		}
		if len(f.Names) == 0 {
			return nil, parseErrorf("cannot inline %s: unnamed parameters are not supported", name)
		}
		for _, n := range f.Names {
			params = append(params, n.Name)
		}
	}
	return params, nil
}

// selectorAndKeyIdents returns idents that must not be treated as parameter
// uses: selector field names. A composite-literal key matching a parameter
// name is ambiguous without type info, so it is refused via walkErr.
func selectorAndKeyIdents(root ast.Node, params map[string]int, walkErr *error) map[*ast.Ident]bool {
	skip := map[*ast.Ident]bool{}
	ast.Inspect(root, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.SelectorExpr:
			skip[v.Sel] = true
		case *ast.KeyValueExpr:
			if id, ok := v.Key.(*ast.Ident); ok {
				if _, isParam := params[id.Name]; isParam && *walkErr == nil {
					*walkErr = parseErrorf("cannot inline: parameter %q is used as a composite-literal key (ambiguous without type info)", id.Name)
				}
			}
		}
		return true
	})
	return skip
}

// substitute renders the template with each parameter occurrence replaced by
// the corresponding argument source text.
func (t *inlineTemplate) substitute(args []string) string {
	out := t.body
	for i := len(t.uses) - 1; i >= 0; i-- {
		u := t.uses[i]
		arg := args[u.param]
		if !isSimpleArgText(arg) {
			arg = "(" + arg + ")"
		}
		out = out[:u.start] + arg + out[u.end:]
	}
	return out
}

// inlineCallSite is one call of the target function in the package.
type inlineCallSite struct {
	file               string
	src                []byte
	line               int
	call               *ast.CallExpr
	start, end         int   // byte range of the call expression
	stmtStart, stmtEnd int   // byte range of the enclosing ExprStmt, or -1
	argStart, argEnd   []int // byte ranges of each argument
}

// collectInlineCallSites finds every call of funcName across the package of
// declFile, refusing value uses, shadowing, external-test-package references,
// and arity mismatches.
func collectInlineCallSites(declFile, pkgName, funcName string, hasResults bool, paramCount int) ([]inlineCallSite, error) {
	var sites []inlineCallSite
	for _, f := range packageGoFiles(declFile) {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if !bytes.Contains(src, []byte(funcName)) {
			continue
		}
		fset := token.NewFileSet()
		node, err := goparser.ParseFile(fset, f, src, goparser.ParseComments)
		if err != nil {
			return nil, parseErrorf("failed to parse %s: %v", f, err)
		}
		if node.Name.Name != pkgName {
			// External test package (pkg_test): selector references would be
			// invisible to the ident scan, so check explicitly and refuse.
			if loc := selectorUseLine(fset, node, funcName); loc != "" {
				return nil, notFoundErrorf("cannot inline %s: referenced from external test package at %s", funcName, loc)
			}
			continue
		}
		fileSites, err := callSitesInFile(fset, node, src, f, funcName, hasResults, paramCount)
		if err != nil {
			return nil, err
		}
		sites = append(sites, fileSites...)
	}
	return sites, nil
}

func markFieldList(fl *ast.FieldList, mark func(*ast.Ident)) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		for _, n := range f.Names {
			mark(n)
		}
	}
}

// selectorUseLine returns "file:line" of the first pkg.Name selector use of
// name in node, or "".
func selectorUseLine(fset *token.FileSet, node *ast.File, name string) string {
	loc := ""
	ast.Inspect(node, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == name && loc == "" {
			p := fset.Position(sel.Pos())
			loc = fmt.Sprintf("%s:%d", p.Filename, p.Line)
		}
		return loc == ""
	})
	return loc
}

// moduleRootOf walks up from dir looking for go.mod.
func moduleRootOf(dir string) string {
	d, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// isSimpleArgText reports whether an argument's source text can be
// substituted without protective parentheses.
func isSimpleArgText(s string) bool {
	expr, err := goparser.ParseExpr(s)
	if err != nil {
		return false
	}
	switch expr.(type) {
	case *ast.Ident, *ast.BasicLit, *ast.SelectorExpr, *ast.ParenExpr, *ast.CompositeLit, *ast.IndexExpr, *ast.CallExpr:
		return true
	}
	return false
}

// isSimpleExprText reports whether the substituted return expression needs
// parentheses when embedded in a caller expression.
func isSimpleExprText(e ast.Expr) bool {
	switch e.(type) {
	case *ast.Ident, *ast.BasicLit, *ast.SelectorExpr, *ast.ParenExpr, *ast.CallExpr, *ast.CompositeLit, *ast.IndexExpr:
		return true
	}
	return false
}
