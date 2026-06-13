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
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

var inlineFlags = mutFlagSpec(nil)

func init() {
	registerCommand(Command{
		Name:        "inline",
		Description: "Inline a simple function into its call sites and delete it (refuses anything complex)",
		Usage:       "inline <file> <Func> [--json] [--dry-run] [--gate]",
		MinArgs:     2,
		MaxArgs:     2,
		Flags:       inlineFlags,
		Run:         inlineCommand,
	})
}

// inlineCommand inlines a function into all its call sites and deletes it.
// MVP scope (a harness refuses rather than corrupts):
//   - the body is a single `return <expr>` or a statement list with no returns
//   - all call sites are in the same package (plus a best-effort scan that
//     refuses when an exported function is referenced from another package)
//   - every argument is side-effect-free and every parameter is used at most
//     once (temp-var introduction is out of scope)
//   - refused outright: multiple return values, named results, variadic or
//     generic functions, defer/go, closures, recursion, use as a value
func inlineCommand(args []string) error {
	pos, flags := parseFlags(args, inlineFlags)
	if len(pos) < 2 {
		return usageErrorf("usage: inline <file> <Func>")
	}
	file := pos[0]
	funcName := pos[1]
	if strings.Contains(funcName, ":") {
		return usageErrorf("inline supports top-level functions only, not methods (got %q)", funcName)
	}

	m := &mutation{op: "inline", file: file, files: packageGoFiles(file)}
	m.setCommonFlags(flags)

	declSrc, err := os.ReadFile(file)
	if err != nil {
		return m.fail(err)
	}
	fset := token.NewFileSet()
	node, err := goparser.ParseFile(fset, file, declSrc, goparser.ParseComments)
	if err != nil {
		return m.fail(parseErrorf("failed to parse %s: %v", file, err))
	}

	var target *ast.FuncDecl
	for _, d := range node.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == funcName {
			target = fd
			break
		}
	}
	if target == nil {
		funcs, _ := declNames(node)
		return m.fail(notFoundError(
			fmt.Sprintf("function %q not found in %s", funcName, file),
			funcName, funcs))
	}

	tmpl, err := buildInlineTemplate(fset, declSrc, target)
	if err != nil {
		return m.fail(err)
	}

	hasResults := target.Type.Results != nil && len(target.Type.Results.List) > 0
	sites, err := collectInlineCallSites(file, node.Name.Name, funcName, hasResults, len(tmpl.params))
	if err != nil {
		return m.fail(err)
	}
	if ast.IsExported(funcName) {
		if loc := findCrossPackageUse(file, node.Name.Name, funcName); loc != "" {
			return m.fail(notFoundErrorf(
				"cannot inline %s: referenced outside its package at %s (all call sites must be in the same package)",
				funcName, loc))
		}
	}

	// Validate arguments and parameter usage per site.
	for _, s := range sites {
		for i, arg := range s.call.Args {
			if !isPureExpr(arg) {
				return m.fail(parseErrorf(
					"cannot inline %s: argument %d at %s:%d may have side effects; temp vars are out of scope — simplify the argument first",
					funcName, i+1, s.file, s.line))
			}
		}
	}

	// Build per-file edit lists.
	edits := map[string][]textEdit{}
	for _, s := range sites {
		argTexts := make([]string, len(s.call.Args))
		for i := range s.call.Args {
			argTexts[i] = string(s.src[s.argStart[i]:s.argEnd[i]])
		}
		text := tmpl.substitute(argTexts)
		start, end := s.start, s.end
		if tmpl.exprMode {
			if s.stmtStart >= 0 {
				// Result discarded at a statement position: keep evaluation.
				start, end = s.stmtStart, s.stmtEnd
				text = "_ = " + text
			} else if !isSimpleExprText(tmpl.returnExpr) {
				text = "(" + text + ")"
			}
		} else {
			start, end = s.stmtStart, s.stmtEnd
		}
		edits[s.file] = append(edits[s.file], textEdit{start: start, end: end, text: text})
	}

	// Delete the declaration (including its doc comment).
	delStart := fset.Position(target.Pos()).Offset
	if target.Doc != nil {
		delStart = fset.Position(target.Doc.Pos()).Offset
	}
	delEnd := fset.Position(target.End()).Offset
	for delEnd < len(declSrc) && declSrc[delEnd] == '\n' {
		delEnd++
	}
	edits[file] = append(edits[file], textEdit{start: delStart, end: delEnd, text: ""})

	// Apply edits in memory, parse-verify every file, then write.
	results := map[string][]byte{}
	for f, list := range edits {
		src, err := os.ReadFile(f)
		if err != nil {
			return m.fail(err)
		}
		out, err := applyTextEdits(src, list)
		if err != nil {
			return m.fail(err)
		}
		if _, perr := goparser.ParseFile(token.NewFileSet(), f, out, 0); perr != nil {
			return m.fail(parseErrorf("inlining %s would produce a malformed file %s: %v", funcName, f, perr))
		}
		results[f] = out
	}

	return m.run(func() (string, error) {
		for f, out := range results {
			if err := os.WriteFile(f, out, 0644); err != nil {
				return "", err
			}
			if err := orchestrator.FormatImports(f); err != nil {
				fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", f, err)
			}
		}
		return fmt.Sprintf("Inlined %s into %d call site(s) and deleted it from %s", funcName, len(sites), file), nil
	})
}

// textEdit is a byte-range replacement within one file.
type textEdit struct {
	start, end int
	text       string
}

func applyTextEdits(src []byte, edits []textEdit) ([]byte, error) {
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

// buildInlineTemplate validates the function shape and extracts the
// substitution template. All refusals are specific exit-3 errors.
func buildInlineTemplate(fset *token.FileSet, src []byte, fd *ast.FuncDecl) (*inlineTemplate, error) {
	name := fd.Name.Name
	if fd.Body == nil {
		return nil, parseErrorf("cannot inline %s: function has no body", name)
	}
	if fd.Type.TypeParams != nil && len(fd.Type.TypeParams.List) > 0 {
		return nil, parseErrorf("cannot inline %s: generic functions are not supported", name)
	}
	params, err := flattenParamNames(fd, name)
	if err != nil {
		return nil, err
	}
	if err := refuseComplexBody(fd, name); err != nil {
		return nil, err
	}

	tmpl := &inlineTemplate{params: params}
	var region ast.Node
	if len(fd.Body.List) == 1 {
		if ret, ok := fd.Body.List[0].(*ast.ReturnStmt); ok {
			if len(ret.Results) != 1 {
				return nil, parseErrorf("cannot inline %s: only single-value returns are supported (got %d results)", name, len(ret.Results))
			}
			tmpl.exprMode = true
			tmpl.returnExpr = ret.Results[0]
			region = ret.Results[0]
		}
	}
	if region == nil {
		// Statement mode: no return statements allowed anywhere.
		hasReturn := false
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if _, ok := n.(*ast.ReturnStmt); ok {
				hasReturn = true
			}
			return true
		})
		if hasReturn {
			return nil, parseErrorf("cannot inline %s: bodies with return statements are only supported as a single `return <expr>`", name)
		}
		if fd.Type.Results != nil && len(fd.Type.Results.List) > 0 {
			return nil, parseErrorf("cannot inline %s: function declares results but has no single return", name)
		}
		if err := refuseStmtModeHazards(fd, name); err != nil {
			return nil, err
		}
		if len(fd.Body.List) == 0 {
			return nil, parseErrorf("cannot inline %s: empty body (use delete --safe instead)", name)
		}
		region = bodyRegion{fd.Body.List[0].Pos(), fd.Body.List[len(fd.Body.List)-1].End()}
	}

	regStart := fset.Position(region.Pos()).Offset
	regEnd := fset.Position(region.End()).Offset
	tmpl.body = string(src[regStart:regEnd])

	paramSet := map[string]int{}
	for i, p := range params {
		paramSet[p] = i
	}
	counts := make([]int, len(params))
	var walkErr error
	skip := selectorAndKeyIdents(regionAST(fd, tmpl.exprMode), paramSet, &walkErr)
	if walkErr != nil {
		return nil, walkErr
	}
	ast.Inspect(regionAST(fd, tmpl.exprMode), func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || skip[id] {
			return true
		}
		idx, isParam := paramSet[id.Name]
		if !isParam {
			return true
		}
		counts[idx]++
		tmpl.uses = append(tmpl.uses, paramUse{
			start: fset.Position(id.Pos()).Offset - regStart,
			end:   fset.Position(id.End()).Offset - regStart,
			param: idx,
		})
		return true
	})
	for i, c := range counts {
		if c > 1 {
			return nil, parseErrorf("cannot inline %s: parameter %q is used %d times; temp vars are out of scope — refusing", name, params[i], c)
		}
	}
	// Taking a parameter's address aliases the caller's variable after
	// substitution — observable semantic change.
	var addrErr error
	ast.Inspect(regionAST(fd, tmpl.exprMode), func(n ast.Node) bool {
		if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.AND {
			if id, ok := u.X.(*ast.Ident); ok {
				if _, isParam := paramSet[id.Name]; isParam && addrErr == nil {
					addrErr = parseErrorf("cannot inline %s: body takes the address of parameter %q", name, id.Name)
				}
			}
		}
		return addrErr == nil
	})
	if addrErr != nil {
		return nil, addrErr
	}
	sort.Slice(tmpl.uses, func(i, j int) bool { return tmpl.uses[i].start < tmpl.uses[j].start })
	return tmpl, nil
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

// refuseComplexBody rejects bodies containing constructs that cannot be
// inlined safely: defer, go, closures, and recursion or self-reference.
func refuseComplexBody(fd *ast.FuncDecl, name string) error {
	var refusal error
	set := func(format string, a ...interface{}) {
		if refusal == nil {
			refusal = parseErrorf(format, a...)
		}
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.DeferStmt:
			set("cannot inline %s: body contains defer", name)
		case *ast.GoStmt:
			set("cannot inline %s: body contains a go statement", name)
		case *ast.FuncLit:
			set("cannot inline %s: body contains a closure", name)
		case *ast.Ident:
			if v.Name == name {
				set("cannot inline %s: function is recursive or refers to itself", name)
			}
		}
		return refusal == nil
	})
	return refusal
}

// refuseStmtModeHazards rejects statement-mode bodies that declare variables,
// assign, or branch — splicing those into a caller scope risks capture.
func refuseStmtModeHazards(fd *ast.FuncDecl, name string) error {
	var refusal error
	set := func(format string, a ...interface{}) {
		if refusal == nil {
			refusal = parseErrorf(format, a...)
		}
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			if v.Tok == token.DEFINE {
				set("cannot inline %s: body declares variables (capture risk in caller scope)", name)
			} else {
				set("cannot inline %s: body assigns to variables", name)
			}
		case *ast.DeclStmt:
			set("cannot inline %s: body declares variables (capture risk in caller scope)", name)
		case *ast.LabeledStmt, *ast.BranchStmt:
			set("cannot inline %s: body contains labels or branch statements", name)
		case *ast.RangeStmt:
			set("cannot inline %s: body contains a range statement (declares variables)", name)
		case *ast.IncDecStmt:
			set("cannot inline %s: body mutates a variable (++/--)", name)
		}
		return refusal == nil
	})
	return refusal
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

// callSitesInFile scans one same-package file for call sites of funcName.
// localDecl is the target's FuncDecl as parsed in this file's AST (nil when
// the declaration lives in another file of the package).
func callSitesInFile(fset *token.FileSet, node *ast.File, src []byte, f, funcName string, hasResults bool, paramCount int) ([]inlineCallSite, error) {
	var localDecl *ast.FuncDecl
	for _, d := range node.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == funcName {
			localDecl = fd
			break
		}
	}
	if shadowLine := findShadowingDecl(fset, node, funcName, localDecl); shadowLine != "" {
		return nil, parseErrorf("cannot inline %s: name is redeclared or shadowed at %s — refusing (cannot distinguish uses)", funcName, shadowLine)
	}

	callFun := map[*ast.Ident]*ast.CallExpr{}
	stmtOf := map[*ast.CallExpr]*ast.ExprStmt{}
	skip := map[*ast.Ident]bool{}
	ast.Inspect(node, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CallExpr:
			if id, ok := v.Fun.(*ast.Ident); ok {
				callFun[id] = v
			}
		case *ast.SelectorExpr:
			skip[v.Sel] = true
		case *ast.BlockStmt:
			for _, s := range v.List {
				if es, ok := s.(*ast.ExprStmt); ok {
					if c, ok := es.X.(*ast.CallExpr); ok {
						stmtOf[c] = es
					}
				}
			}
		case *ast.CaseClause:
			for _, s := range v.Body {
				if es, ok := s.(*ast.ExprStmt); ok {
					if c, ok := es.X.(*ast.CallExpr); ok {
						stmtOf[c] = es
					}
				}
			}
		case *ast.CommClause:
			for _, s := range v.Body {
				if es, ok := s.(*ast.ExprStmt); ok {
					if c, ok := es.X.(*ast.CallExpr); ok {
						stmtOf[c] = es
					}
				}
			}
		}
		return true
	})

	var sites []inlineCallSite
	var refusal error
	ast.Inspect(node, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || id.Name != funcName || skip[id] || refusal != nil {
			return true
		}
		if localDecl != nil && id == localDecl.Name {
			return true // the declaration itself
		}
		pos := fset.Position(id.Pos())
		call, isCall := callFun[id]
		if !isCall {
			refusal = parseErrorf("cannot inline %s: used as a value (not called) at %s:%d", funcName, f, pos.Line)
			return true
		}
		if call.Ellipsis.IsValid() {
			refusal = parseErrorf("cannot inline %s: call at %s:%d uses ... expansion", funcName, f, pos.Line)
			return true
		}
		if len(call.Args) != paramCount {
			refusal = parseErrorf("cannot inline %s: call at %s:%d passes %d arg(s), function has %d parameter(s)", funcName, f, pos.Line, len(call.Args), paramCount)
			return true
		}
		site := inlineCallSite{
			file:      f,
			src:       src,
			line:      pos.Line,
			call:      call,
			start:     fset.Position(call.Pos()).Offset,
			end:       fset.Position(call.End()).Offset,
			stmtStart: -1,
			stmtEnd:   -1,
		}
		if es, ok := stmtOf[call]; ok {
			site.stmtStart = fset.Position(es.Pos()).Offset
			site.stmtEnd = fset.Position(es.End()).Offset
		}
		for _, a := range call.Args {
			site.argStart = append(site.argStart, fset.Position(a.Pos()).Offset)
			site.argEnd = append(site.argEnd, fset.Position(a.End()).Offset)
		}
		sites = append(sites, site)
		return true
	})
	if refusal != nil {
		return nil, refusal
	}

	// Statement-mode bodies require every call to sit in statement position
	// directly inside a block or case body.
	for _, s := range sites {
		if !hasResults && s.stmtStart < 0 {
			return nil, parseErrorf("cannot inline %s: call at %s:%d is not in statement position", funcName, f, s.line)
		}
	}
	return sites, nil
}

// findShadowingDecl reports the location of any declaration (other than the
// target function itself) introducing the name in this file.
func findShadowingDecl(fset *token.FileSet, node *ast.File, name string, target *ast.FuncDecl) string {
	loc := ""
	mark := func(id *ast.Ident) {
		if id.Name == name && loc == "" {
			p := fset.Position(id.Pos())
			loc = fmt.Sprintf("%s:%d", p.Filename, p.Line)
		}
	}
	ast.Inspect(node, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncDecl:
			if v != target {
				if v.Recv == nil {
					mark(v.Name)
				}
				markFieldList(v.Type.Params, mark)
				markFieldList(v.Type.Results, mark)
				markFieldList(v.Recv, mark)
			}
		case *ast.FuncLit:
			markFieldList(v.Type.Params, mark)
			markFieldList(v.Type.Results, mark)
		case *ast.ValueSpec:
			for _, id := range v.Names {
				mark(id)
			}
		case *ast.TypeSpec:
			mark(v.Name)
		case *ast.AssignStmt:
			if v.Tok == token.DEFINE {
				for _, lhs := range v.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						mark(id)
					}
				}
			}
		case *ast.RangeStmt:
			if v.Tok == token.DEFINE {
				if id, ok := v.Key.(*ast.Ident); ok {
					mark(id)
				}
				if id, ok := v.Value.(*ast.Ident); ok {
					mark(id)
				}
			}
		}
		return loc == ""
	})
	return loc
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

// findCrossPackageUse scans the module for selector references to an
// exported function from other packages (best-effort: matches pkgName.Name).
func findCrossPackageUse(declFile, pkgName, funcName string) string {
	root := moduleRootOf(filepath.Dir(declFile))
	if root == "" {
		return ""
	}
	pkgDir, _ := filepath.Abs(filepath.Dir(declFile))
	loc := ""
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || loc != "" {
			return filepath.SkipAll
		}
		if info.IsDir() {
			base := info.Name()
			if base == "vendor" || base == ".git" || strings.HasPrefix(base, ".") && base != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if abs, _ := filepath.Abs(filepath.Dir(path)); abs == pkgDir {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil || !bytes.Contains(src, []byte(funcName)) || !bytes.Contains(src, []byte(pkgName)) {
			return nil
		}
		fset := token.NewFileSet()
		node, perr := goparser.ParseFile(fset, path, src, 0)
		if perr != nil {
			return nil
		}
		ast.Inspect(node, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != funcName || loc != "" {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == pkgName {
				p := fset.Position(sel.Pos())
				loc = fmt.Sprintf("%s:%d", p.Filename, p.Line)
			}
			return loc == ""
		})
		return nil
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

// isPureExpr reports whether evaluating e is side-effect-free (no calls,
// channel ops, or closures). Pure expressions may be substituted textually.
func isPureExpr(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident, *ast.BasicLit:
		return true
	case *ast.SelectorExpr:
		return isPureExpr(v.X)
	case *ast.ParenExpr:
		return isPureExpr(v.X)
	case *ast.StarExpr:
		return isPureExpr(v.X)
	case *ast.UnaryExpr:
		return v.Op != token.ARROW && isPureExpr(v.X)
	case *ast.BinaryExpr:
		return isPureExpr(v.X) && isPureExpr(v.Y)
	case *ast.IndexExpr:
		return isPureExpr(v.X) && isPureExpr(v.Index)
	case *ast.SliceExpr:
		for _, sub := range []ast.Expr{v.X, v.Low, v.High, v.Max} {
			if sub != nil && !isPureExpr(sub) {
				return false
			}
		}
		return true
	case *ast.CompositeLit:
		for _, elt := range v.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if !isPureExpr(kv.Value) {
					return false
				}
				continue
			}
			if !isPureExpr(elt) {
				return false
			}
		}
		return true
	}
	return false
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
