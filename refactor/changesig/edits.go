package changesig

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/chrisophus/gorefactor/internal/cerr"
	"github.com/chrisophus/gorefactor/internal/goload"
	"github.com/chrisophus/gorefactor/orchestrator"

	"golang.org/x/tools/go/packages"
)

// TextEdit is a byte-range replacement in a file. Offsets come from the shared
// token.FileSet of a packages.Load run.
type TextEdit struct {
	file  string
	start int
	end   int
	text  string
}

// EditedFiles returns the distinct files touched by edits, sorted.
func EditedFiles(edits []TextEdit) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range edits {
		if !seen[e.file] {
			seen[e.file] = true
			out = append(out, e.file)
		}
	}
	sort.Strings(out)
	return out
}

// Apply applies edits grouped per file (descending offset so earlier offsets
// stay valid), parse-checks every result before writing, then runs goimports on
// each touched file.
func Apply(edits []TextEdit) error {
	byFile := map[string][]TextEdit{}
	seen := map[string]bool{}
	for _, e := range edits {
		key := fmt.Sprintf("%s:%d:%d:%s", e.file, e.start, e.end, e.text)
		if seen[key] {
			continue // same edit discovered via multiple package variants
		}
		seen[key] = true
		byFile[e.file] = append(byFile[e.file], e)
	}
	for _, file := range EditedFiles(edits) {
		list := byFile[file]
		sort.Slice(list, func(i, j int) bool { return list[i].start > list[j].start })
		src, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}
		for _, e := range list {
			if e.start < 0 || e.end > len(src) || e.start > e.end {
				return fmt.Errorf("edit out of range in %s (%d-%d)", file, e.start, e.end)
			}
			src = append(src[:e.start], append([]byte(e.text), src[e.end:]...)...)
		}
		fset := token.NewFileSet()
		if _, perr := goparser.ParseFile(fset, file, src, 0); perr != nil {
			return cerr.Parsef("internal: rewrite of %s does not parse, refusing to write: %v", file, perr)
		}
		if err := os.WriteFile(file, src, 0644); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
		if err := orchestrator.FormatImports(file); err != nil {
			fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
		}
	}
	return nil
}

// sigParam is one flattened parameter ("a, b int" yields two entries).
type sigParam struct {
	name      string
	nameIdent *ast.Ident
	typeExpr  ast.Expr
	variadic  bool
}

func flattenParams(fl *ast.FieldList) []sigParam {
	var out []sigParam
	if fl == nil {
		return out
	}
	for _, f := range fl.List {
		_, variadic := f.Type.(*ast.Ellipsis)
		if len(f.Names) == 0 {
			out = append(out, sigParam{typeExpr: f.Type, variadic: variadic})
			continue
		}
		for _, n := range f.Names {
			out = append(out, sigParam{name: n.Name, nameIdent: n, typeExpr: f.Type, variadic: variadic})
		}
	}
	return out
}

func paramNames(params []sigParam) []string {
	var out []string
	for i, p := range params {
		if p.name != "" {
			out = append(out, p.name)
		} else {
			out = append(out, strconv.Itoa(i))
		}
	}
	return out
}

func variadicIndexOf(params []sigParam) int {
	for i, p := range params {
		if p.variadic {
			return i
		}
	}
	return -1
}

func exprText(fset *token.FileSet, e ast.Expr) string {
	var b strings.Builder
	_ = printer.Fprint(&b, fset, e)
	return b.String()
}

// paramTexts renders flattened params as "name type" strings. forceNamed names
// unnamed params "_" so a named param can join the list legally.
func paramTexts(fset *token.FileSet, params []sigParam, forceNamed bool) []string {
	named := forceNamed
	for _, p := range params {
		if p.name != "" {
			named = true
		}
	}
	var parts []string
	for _, p := range params {
		t := exprText(fset, p.typeExpr)
		if named {
			n := p.name
			if n == "" {
				n = "_"
			}
			parts = append(parts, n+" "+t)
		} else {
			parts = append(parts, t)
		}
	}
	return parts
}

// signatureEdit replaces the whole parameter list "(...)" of fn.
func signatureEdit(tgt *sigTarget, parts []string) TextEdit {
	fset := tgt.pkg.Fset
	open := fset.Position(tgt.fn.Type.Params.Opening)
	closing := fset.Position(tgt.fn.Type.Params.Closing)
	return TextEdit{
		file:  open.Filename,
		start: open.Offset,
		end:   closing.Offset + 1,
		text:  "(" + strings.Join(parts, ", ") + ")",
	}
}

func buildSignatureEdits(pkgs []*packages.Package, tgt *sigTarget, action *Action, locator string) ([]TextEdit, string, error) {
	params := flattenParams(tgt.fn.Type.Params)
	switch action.Kind {
	case "rename":
		return buildRenameParamEdits(tgt, params, action, locator)
	case "add":
		return buildAddParamEdits(pkgs, tgt, params, action, locator)
	case "remove":
		return buildRemoveParamEdits(pkgs, tgt, params, action, locator)
	}
	return nil, "", cerr.Usagef("change-signature: unknown action %q", action.Kind)
}

// identEdit replaces a single identifier occurrence.
func identEdit(fset *token.FileSet, id *ast.Ident, newName string) TextEdit {
	pos := fset.Position(id.Pos())
	return TextEdit{file: pos.Filename, start: pos.Offset, end: pos.Offset + len(id.Name), text: newName}
}

// paramUseEdits finds identifiers in fn's body resolving to the parameter
// declared at defPos and returns one edit per use (or just the positions when
// newName is empty).
func paramUseEdits(tgt *sigTarget, defPos token.Pos, newName string) (edits []TextEdit, locs []string) {
	if tgt.fn.Body == nil {
		return nil, nil
	}
	info := tgt.pkg.TypesInfo
	fset := tgt.pkg.Fset
	ast.Inspect(tgt.fn.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		obj := info.Uses[id]
		if obj == nil || obj.Pos() != defPos {
			return true
		}
		p := fset.Position(id.Pos())
		locs = append(locs, fmt.Sprintf("%s:%d", p.Filename, p.Line))
		if newName != "" {
			edits = append(edits, identEdit(fset, id, newName))
		}
		return true
	})
	return edits, locs
}

func buildRenameParamEdits(tgt *sigTarget, params []sigParam, action *Action, locator string) ([]TextEdit, string, error) {
	var target *sigParam
	for i := range params {
		if params[i].name == action.OldName {
			target = &params[i]
		}
		if params[i].name == action.NewName {
			return nil, "", cerr.Usagef("parameter %q already exists on %s", action.NewName, locator)
		}
	}
	if target == nil {
		return nil, "", cerr.NotFound(
			fmt.Sprintf("parameter %q not found on %s", action.OldName, locator),
			action.OldName, paramNames(params))
	}
	if rn := receiverName(tgt.fn); rn != "" && rn == action.NewName {
		return nil, "", cerr.Usagef("parameter name %q collides with the receiver name", action.NewName)
	}
	edits := []TextEdit{identEdit(tgt.pkg.Fset, target.nameIdent, action.NewName)}
	useEdits, locs := paramUseEdits(tgt, target.nameIdent.Pos(), action.NewName)
	edits = append(edits, useEdits...)
	detail := fmt.Sprintf("Renamed parameter %s -> %s on %s (%d use(s) in body)",
		action.OldName, action.NewName, locator, len(locs))
	return edits, detail, nil
}

func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 || len(fn.Recv.List[0].Names) == 0 {
		return ""
	}
	return fn.Recv.List[0].Names[0].Name
}

// resolveParamRef resolves a --remove-param argument (name or 0-based index).
func resolveParamRef(params []sigParam, ref, locator string) (int, error) {
	if n, err := strconv.Atoi(ref); err == nil {
		if n < 0 || n >= len(params) {
			return -1, cerr.NotFoundf("parameter index %d out of range on %s (0-%d)", n, locator, len(params)-1)
		}
		return n, nil
	}
	for i, p := range params {
		if p.name == ref {
			return i, nil
		}
	}
	return -1, cerr.NotFound(
		fmt.Sprintf("parameter %q not found on %s", ref, locator),
		ref, paramNames(params))
}

// callValueFor resolves the default argument expression for a new parameter.
func callValueFor(tgt *sigTarget, action *Action) (string, error) {
	if action.CallValue != "" {
		return action.CallValue, nil
	}
	var typ types.Type
	if tv, err := types.Eval(tgt.pkg.Fset, tgt.pkg.Types, tgt.fn.Pos(), action.ParamType); err == nil && tv.IsType() {
		typ = tv.Type
	}
	v := goload.ZeroValueExpr(typ, action.ParamType)
	if v == "" {
		return "", cerr.Usagef("cannot infer a zero value for type %s; pass --call-value EXPR", action.ParamType)
	}
	return v, nil
}
