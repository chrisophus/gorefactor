package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"sort"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

var extractVarFlags = mutFlagSpec(map[string]bool{"--all": false})

func init() {
	registerCommand(Command{
		Name:        "extract-var",
		Description: "Extract an expression inside a function into a named local variable (name := expr)",
		Usage:       "extract-var <file> <Func|Receiver:Method> <expr> <name> [--all] [--json] [--dry-run] [--gate]",
		MinArgs:     4,
		MaxArgs:     4,
		Flags:       extractVarFlags,
		Run:         extractVarCommand,
	})
	registerCommand(Command{
		Name:        "extract-const",
		Description: "Extract a constant expression inside a function into a named local const (const name = expr)",
		Usage:       "extract-const <file> <Func|Receiver:Method> <expr> <name> [--all] [--json] [--dry-run] [--gate]",
		MinArgs:     4,
		MaxArgs:     4,
		Flags:       extractVarFlags,
		Run:         extractConstCommand,
	})
}

func extractVarCommand(args []string) error   { return runExtractVar(args, false) }
func extractConstCommand(args []string) error { return runExtractVar(args, true) }

// runExtractVar introduces a `name := expr` (or `const name = expr`) binding
// immediately before the statement where expr first appears in the target
// function, and rewrites that occurrence (or every textual occurrence with
// --all) to reference name. The binding is inserted into the SAME block as the
// occurrence — descending into nested if/for/switch bodies — so the expression
// is still evaluated at the same point (identical side-effect timing and
// variable scope). Single-occurrence extraction is therefore always
// behaviour-preserving; --all additionally assumes the expression is pure and
// its inputs are unchanged between occurrences, which the tool cannot prove —
// gate it with --gate when in doubt.
func runExtractVar(args []string, isConst bool) error {
	op := "extract-var"
	if isConst {
		op = "extract-const"
	}
	pos, flags := parseFlags(args, extractVarFlags)
	if len(pos) < 4 {
		return usageErrorf("usage: %s <file> <Func|Receiver:Method> <expr> <name> [--all]", op)
	}
	file, locator, exprText, name := pos[0], pos[1], pos[2], pos[3]
	all := flags["--all"] != ""

	m := &mutation{op: op, file: file}
	m.setCommonFlags(flags)

	if !isValidIdent(name) {
		return m.fail(usageErrorf("%q is not a valid Go identifier for the new %s name", name, bindingKind(isConst)))
	}

	wantExpr, err := parser.ParseExpr(exprText)
	if err != nil {
		return m.fail(parseErrorf("expression %q does not parse: %v", exprText, err))
	}
	wantCanon := canonicalExpr(wantExpr)

	src, fset, target, err := loadFuncTarget(file, locator)
	if err != nil {
		return m.fail(err)
	}

	if isConst {
		locals := collectLocalVarNames(target)
		if bad := nonConstReason(wantExpr, locals); bad != "" {
			return m.fail(usageErrorf("expression %q cannot be a constant: %s (use extract-var instead)", exprText, bad))
		}
	}

	matches := matchingExprs(target.Body, wantCanon)
	if len(matches) == 0 {
		return m.fail(notFoundErrorf(
			"expression %q not found inside %s\nhint: the pattern is matched as a whole Go expression (whitespace-insensitive), not raw text; check it appears in the body",
			exprText, locator))
	}
	if isValidIdent(exprText) && exprText == name {
		return m.fail(usageErrorf("new name %q is identical to the extracted expression", name))
	}

	anchor := findAnchorStmt(target.Body, matches[0])
	if anchor == nil {
		return m.fail(fmt.Errorf("could not locate a statement enclosing the expression in %s", locator))
	}

	out, rewritten := planExtractEdits(src, fset, anchor, matches, name, isConst, all)
	detail := fmt.Sprintf("Extracted %s %q from %s (%d occurrence(s) rewritten)",
		bindingKind(isConst), name, locator, rewritten)

	return m.run(func() (string, error) {
		if err := os.WriteFile(file, out, 0644); err != nil {
			return "", err
		}
		if _, perr := parser.ParseFile(token.NewFileSet(), file, out, parser.SkipObjectResolution); perr != nil {
			return "", parseErrorf("extraction would make %s unparseable: %v", file, perr)
		}
		if err := orchestrator.FormatImports(file); err != nil {
			fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
		}
		return detail, nil
	})
}

// loadFuncTarget reads and parses file, then locates the function/method named
// by locator, returning its source, fileset, and declaration. The returned
// error is already shaped for the CLI (not-found with candidates, parse error).
func loadFuncTarget(file, locator string) ([]byte, *token.FileSet, *ast.FuncDecl, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, nil, nil, err
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, file, src, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, parseErrorf("failed to parse %s: %v", file, err)
	}
	funcName, methodName, receiverType := parseLocatorParts(locator)
	target := orchestrator.NewCodeInserter().FindFunction(node, funcName, methodName, receiverType)
	if target == nil || target.Body == nil {
		funcs, _ := declNames(node)
		return nil, nil, nil, notFoundError(
			fmt.Sprintf("target function %q not found or has no body in %s", locator, file),
			locator, funcs)
	}
	return src, fset, target, nil
}

// planExtractEdits produces the rewritten file bytes: a binding inserted at the
// anchor statement's line start, plus a substitution over the first occurrence
// (or every occurrence when all is set). Edits are applied high-offset first so
// earlier offsets stay valid. It returns the new bytes and how many occurrences
// were rewritten.
func planExtractEdits(src []byte, fset *token.FileSet, anchor ast.Stmt, matches []ast.Expr, name string, isConst, all bool) ([]byte, int) {
	anchorStart := fset.Position(anchor.Pos()).Offset
	lineStart := lineStartOffset(src, anchorStart)
	indent := string(src[lineStart:anchorStart])
	rhs := string(src[fset.Position(matches[0].Pos()).Offset:fset.Position(matches[0].End()).Offset])
	decl := declLine(isConst, name, rhs)

	type edit struct {
		start, end int
		text       string
	}
	edits := []edit{{start: lineStart, end: lineStart, text: indent + decl + "\n"}}

	replaceTargets := matches[:1]
	if all {
		replaceTargets = matches
	}
	for _, e := range replaceTargets {
		edits = append(edits, edit{
			start: fset.Position(e.Pos()).Offset,
			end:   fset.Position(e.End()).Offset,
			text:  name,
		})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })

	out := append([]byte{}, src...)
	for _, e := range edits {
		out = append(out[:e.start], append([]byte(e.text), out[e.end:]...)...)
	}
	return out, len(replaceTargets)
}

func bindingKind(isConst bool) string {
	if isConst {
		return "const"
	}
	return "variable"
}

func declLine(isConst bool, name, rhs string) string {
	if isConst {
		return fmt.Sprintf("const %s = %s", name, rhs)
	}
	return fmt.Sprintf("%s := %s", name, rhs)
}

// canonicalExpr renders an expression to gofmt-canonical text (positions
// discarded) so two spellings of the same expression compare equal.
func canonicalExpr(e ast.Expr) string {
	var b strings.Builder
	_ = printer.Fprint(&b, token.NewFileSet(), e)
	return b.String()
}

// matchingExprs returns, in source order, every expression node inside body
// whose canonical form equals wantCanon. Nodes inside nested expressions are
// included; identical text implies structurally identical subtrees.
func matchingExprs(body *ast.BlockStmt, wantCanon string) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(body, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok && canonicalExpr(e) == wantCanon {
			out = append(out, e)
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Pos() < out[j].Pos() })
	return out
}

// findAnchorStmt returns the innermost statement that encloses target without
// target being nested inside one of that statement's child blocks. Inserting a
// binding immediately before this statement preserves the expression's
// evaluation point, scope, and side-effect timing.
func findAnchorStmt(body *ast.BlockStmt, target ast.Expr) ast.Stmt {
	return anchorInList(body.List, target)
}

func anchorInList(list []ast.Stmt, target ast.Expr) ast.Stmt {
	for _, s := range list {
		if !nodeContains(s, target) {
			continue
		}
		for _, child := range childStmtLists(s) {
			if deeper := anchorInList(child, target); deeper != nil {
				return deeper
			}
		}
		return s
	}
	return nil
}

// childStmtLists returns the nested statement lists (block bodies, case bodies,
// else branches) of s into which the anchor search should descend.
func childStmtLists(s ast.Stmt) [][]ast.Stmt {
	var lists [][]ast.Stmt
	add := func(b *ast.BlockStmt) {
		if b != nil {
			lists = append(lists, b.List)
		}
	}
	switch st := s.(type) {
	case *ast.BlockStmt:
		add(st)
	case *ast.IfStmt:
		add(st.Body)
		if st.Else != nil {
			lists = append(lists, []ast.Stmt{st.Else})
		}
	case *ast.ForStmt:
		add(st.Body)
	case *ast.RangeStmt:
		add(st.Body)
	case *ast.SwitchStmt:
		if st.Body != nil {
			for _, c := range st.Body.List {
				if cc, ok := c.(*ast.CaseClause); ok {
					lists = append(lists, cc.Body)
				}
			}
		}
	case *ast.TypeSwitchStmt:
		if st.Body != nil {
			for _, c := range st.Body.List {
				if cc, ok := c.(*ast.CaseClause); ok {
					lists = append(lists, cc.Body)
				}
			}
		}
	case *ast.SelectStmt:
		if st.Body != nil {
			for _, c := range st.Body.List {
				if cc, ok := c.(*ast.CommClause); ok {
					lists = append(lists, cc.Body)
				}
			}
		}
	case *ast.LabeledStmt:
		lists = append(lists, []ast.Stmt{st.Stmt})
	}
	return lists
}

func nodeContains(outer ast.Node, inner ast.Node) bool {
	return outer.Pos() <= inner.Pos() && inner.End() <= outer.End()
}

// lineStartOffset returns the offset of the first byte of the line containing
// off.
func lineStartOffset(src []byte, off int) int {
	i := off
	for i > 0 && src[i-1] != '\n' {
		i--
	}
	return i
}

func isValidIdent(s string) bool {
	if s == "" || s == "_" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return !token.Lookup(s).IsKeyword()
}

// nonConstReason returns a human reason when e cannot be a constant
// expression, or "" when it is plausibly constant. It rejects syntactic
// non-constants (calls, indexing, composite/func literals, address-of, channel
// receive) and any identifier that names a local variable or parameter of the
// enclosing function (locals). It stays conservative on identifiers it can't
// classify — a package-level `var` used as an operand still passes here and is
// caught by the build gate; recommend --gate for extract-const.
func nonConstReason(e ast.Expr, locals map[string]bool) string {
	var reason string
	ast.Inspect(e, func(n ast.Node) bool {
		if reason != "" {
			return false
		}
		switch t := n.(type) {
		case *ast.CallExpr:
			reason = "it contains a function call"
		case *ast.CompositeLit:
			reason = "it contains a composite literal"
		case *ast.FuncLit:
			reason = "it contains a function literal"
		case *ast.IndexExpr, *ast.IndexListExpr:
			reason = "it contains an index expression"
		case *ast.SliceExpr:
			reason = "it contains a slice expression"
		case *ast.TypeAssertExpr:
			reason = "it contains a type assertion"
		case *ast.UnaryExpr:
			if t.Op == token.AND || t.Op == token.ARROW {
				reason = "it takes an address or receives from a channel"
			}
		case *ast.Ident:
			if locals[t.Name] {
				reason = fmt.Sprintf("it references the local variable %q", t.Name)
			}
		}
		return true
	})
	return reason
}

// collectLocalVarNames gathers the names of parameters, receiver, named
// results, and any identifier assigned or var-declared inside fn. A name in
// this set is definitely not a constant, so an extract-const expression
// referencing one is rejected. Selector fields (x.Y) are excluded because only
// the leftmost identifier of a selector names a binding.
func collectLocalVarNames(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	addFields := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			for _, n := range f.Names {
				names[n.Name] = true
			}
		}
	}
	addFields(fn.Recv)
	if fn.Type != nil {
		addFields(fn.Type.Params)
		addFields(fn.Type.Results)
	}
	if fn.Body == nil {
		return names
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range s.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					names[id.Name] = true
				}
			}
		case *ast.RangeStmt:
			if id, ok := s.Key.(*ast.Ident); ok {
				names[id.Name] = true
			}
			if id, ok := s.Value.(*ast.Ident); ok {
				names[id.Name] = true
			}
		case *ast.DeclStmt:
			if gd, ok := s.Decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
				for _, spec := range gd.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok {
						for _, id := range vs.Names {
							names[id.Name] = true
						}
					}
				}
			}
		}
		return true
	})
	return names
}
