package main

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"sort"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

var addFieldFlags = mutFlagSpec(map[string]bool{
	"--after":           true,
	"--update-literals": false,
})

func init() {
	registerCommand(Command{
		Name:        "add-field",
		Description: "Add a field to a struct type; optionally rewrite positional literals to keyed form",
		Usage:       "add-field <file> <Struct> \"<Name> <Type> [`tag`]\" [--after FieldName] [--update-literals] [--json] [--dry-run] [--gate]",
		MinArgs:     3,
		MaxArgs:     3,
		Flags:       addFieldFlags,
		Run:         addFieldCommand,
	})
}

// addFieldCommand adds a field to a struct. Positional (unkeyed) composite
// literals of the struct elsewhere in the package would stop compiling, so
// the command detects them: with --update-literals they are rewritten to
// keyed form first; without it they are listed as warnings and the field is
// added anyway.
func addFieldCommand(args []string) error {
	pos, flags := parseFlags(args, addFieldFlags)
	if len(pos) < 3 {
		return usageErrorf("usage: add-field <file> <Struct> \"<Name> <Type> [`tag`]\" [--after FieldName] [--update-literals]")
	}
	file := pos[0]
	structName := pos[1]
	fieldSpec := strings.TrimSpace(pos[2])
	afterField := flags["--after"]
	updateLiterals := flags["--update-literals"] != ""

	m := &mutation{op: "add-field", file: file, files: packageGoFiles(file)}
	m.setCommonFlags(flags)

	if err := validateFieldSpec(fieldSpec); err != nil {
		return m.fail(err)
	}

	src, err := os.ReadFile(file)
	if err != nil {
		return m.fail(err)
	}
	fset := token.NewFileSet()
	node, err := goparser.ParseFile(fset, file, src, goparser.ParseComments)
	if err != nil {
		return m.fail(parseErrorf("failed to parse %s: %v", file, err))
	}

	st := findStructType(node, structName)
	if st == nil {
		_, all, derr := fileDecls(file)
		if derr != nil {
			all = nil
		}
		return m.fail(notFoundError(
			fmt.Sprintf("struct %q not found in %s", structName, file),
			structName, all))
	}

	fieldNames := structFieldNames(st)
	insertOffset := fset.Position(st.Fields.Closing).Offset
	insertText := "\t" + fieldSpec + "\n"
	if afterField != "" {
		f := findStructField(st, afterField)
		if f == nil {
			return m.fail(notFoundError(
				fmt.Sprintf("field %q not found in struct %s", afterField, structName),
				afterField, fieldNames))
		}
		insertOffset = fset.Position(f.End()).Offset
		insertText = "\n\t" + fieldSpec
	}

	// Detect positional literals across the package before mutating anything.
	positional, err := findPositionalLiterals(file, structName)
	if err != nil {
		return m.fail(err)
	}
	if len(positional) > 0 && !updateLiterals {
		fmt.Fprintf(os.Stderr, "warning: %d positional literal(s) of %s will not compile after adding a field (use --update-literals):\n", len(positional), structName)
		for _, p := range positional {
			fmt.Fprintf(os.Stderr, "  %s:%d\n", p.file, p.line)
		}
	}
	if updateLiterals {
		for _, p := range positional {
			if len(p.elts) != len(fieldNames) {
				return m.fail(fmt.Errorf(
					"cannot rewrite positional literal at %s:%d: %d value(s) but struct %s has %d field(s)",
					p.file, p.line, len(p.elts), structName, len(fieldNames)))
			}
		}
	}

	return m.run(func() (string, error) {
		// Rewrite positional literals first (in other files and possibly in
		// this file); offsets in `file` shift, so re-read before adding the
		// field.
		rewritten := 0
		if updateLiterals && len(positional) > 0 {
			n, err := rewritePositionalLiterals(positional, fieldNames)
			if err != nil {
				return "", err
			}
			rewritten = n
		}

		cur, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		off := insertOffset
		if updateLiterals && rewritten > 0 {
			// Re-locate the struct after literal rewrites in the same file.
			fset := token.NewFileSet()
			node, perr := goparser.ParseFile(fset, file, cur, goparser.ParseComments)
			if perr != nil {
				return "", parseErrorf("re-parse %s after literal rewrite: %v", file, perr)
			}
			st := findStructType(node, structName)
			if st == nil {
				return "", fmt.Errorf("struct %s disappeared after literal rewrite", structName)
			}
			if afterField != "" {
				f := findStructField(st, afterField)
				if f == nil {
					return "", fmt.Errorf("field %s disappeared after literal rewrite", afterField)
				}
				off = fset.Position(f.End()).Offset
			} else {
				off = fset.Position(st.Fields.Closing).Offset
			}
		}

		var out []byte
		out = append(out, cur[:off]...)
		out = append(out, []byte(insertText)...)
		out = append(out, cur[off:]...)
		if _, perr := goparser.ParseFile(token.NewFileSet(), file, out, 0); perr != nil {
			return "", parseErrorf("adding field would produce a malformed file: %v", perr)
		}
		if err := os.WriteFile(file, out, 0644); err != nil {
			return "", err
		}
		if err := orchestrator.FormatImports(file); err != nil {
			fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
		}

		detail := fmt.Sprintf("Added field %q to struct %s in %s", fieldSpec, structName, file)
		if rewritten > 0 {
			detail += fmt.Sprintf(" (rewrote %d positional literal(s) to keyed form)", rewritten)
		} else if len(positional) > 0 {
			detail += fmt.Sprintf(" (%d positional literal(s) left unkeyed — see warnings)", len(positional))
		}
		return detail, nil
	})
}

// validateFieldSpec checks that spec parses as exactly one struct field.
func validateFieldSpec(spec string) error {
	src := "package p\ntype t struct {\n" + spec + "\n}"
	node, err := goparser.ParseFile(token.NewFileSet(), "field.go", src, 0)
	if err != nil {
		return parseErrorf("field spec %q does not parse as a struct field: %v", spec, err)
	}
	for _, d := range node.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, s := range gd.Specs {
			ts, ok := s.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil || len(st.Fields.List) != 1 {
				return parseErrorf("field spec %q must declare exactly one field", spec)
			}
			return nil
		}
	}
	return parseErrorf("field spec %q does not parse as a struct field", spec)
}

// findStructType returns the StructType declared as a top-level `type Name
// struct { ... }` in node, or nil.
func findStructType(node *ast.File, name string) *ast.StructType {
	for _, d := range node.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, s := range gd.Specs {
			ts, ok := s.(*ast.TypeSpec)
			if !ok || ts.Name.Name != name {
				continue
			}
			if st, ok := ts.Type.(*ast.StructType); ok {
				return st
			}
		}
	}
	return nil
}

// structFieldNames returns the field names of a struct in declaration order,
// flattening multi-name fields and using the type name for embedded fields.
func structFieldNames(st *ast.StructType) []string {
	var names []string
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			if n := receiverTypeName(f.Type); n != "" {
				names = append(names, n)
			} else if sel, ok := f.Type.(*ast.SelectorExpr); ok {
				names = append(names, sel.Sel.Name)
			}
			continue
		}
		for _, n := range f.Names {
			names = append(names, n.Name)
		}
	}
	return names
}

func findStructField(st *ast.StructType, name string) *ast.Field {
	for _, f := range st.Fields.List {
		for _, n := range f.Names {
			if n.Name == name {
				return f
			}
		}
		if len(f.Names) == 0 && receiverTypeName(f.Type) == name {
			return f
		}
	}
	return nil
}

// positionalLiteral records one unkeyed composite literal of the target
// struct: the file containing it, its line, and the byte offsets of its
// element expressions (where field keys must be inserted).
type positionalLiteral struct {
	file string
	line int
	elts []int // byte offset of each element's start
}

// findPositionalLiterals scans every file in the package of structFile for
// composite literals `StructName{...}` whose elements are unkeyed. Literals
// with an elided type (e.g. inside `[]StructName{{...}}`) are not detected —
// that requires full type information.
func findPositionalLiterals(structFile, structName string) ([]positionalLiteral, error) {
	var found []positionalLiteral
	for _, f := range packageGoFiles(structFile) {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		node, err := goparser.ParseFile(fset, f, src, goparser.ParseComments)
		if err != nil {
			return nil, parseErrorf("failed to parse %s: %v", f, err)
		}
		ast.Inspect(node, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok || len(cl.Elts) == 0 {
				return true
			}
			id, ok := cl.Type.(*ast.Ident)
			if !ok || id.Name != structName {
				return true
			}
			if _, keyed := cl.Elts[0].(*ast.KeyValueExpr); keyed {
				return true
			}
			lit := positionalLiteral{file: f, line: fset.Position(cl.Pos()).Line}
			for _, e := range cl.Elts {
				lit.elts = append(lit.elts, fset.Position(e.Pos()).Offset)
			}
			found = append(found, lit)
			return true
		})
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].file != found[j].file {
			return found[i].file < found[j].file
		}
		return found[i].line < found[j].line
	})
	return found, nil
}

// rewritePositionalLiterals inserts `FieldName: ` before each element of the
// recorded literals, converting them to keyed form. Returns the number of
// literals rewritten.
func rewritePositionalLiterals(lits []positionalLiteral, fieldNames []string) (int, error) {
	type insert struct {
		offset int
		text   string
	}
	byFile := map[string][]insert{}
	for _, lit := range lits {
		for i, off := range lit.elts {
			byFile[lit.file] = append(byFile[lit.file], insert{offset: off, text: fieldNames[i] + ": "})
		}
	}
	count := 0
	for f, inserts := range byFile {
		src, err := os.ReadFile(f)
		if err != nil {
			return count, err
		}
		sort.Slice(inserts, func(i, j int) bool { return inserts[i].offset > inserts[j].offset })
		out := src
		for _, ins := range inserts {
			var next []byte
			next = append(next, out[:ins.offset]...)
			next = append(next, []byte(ins.text)...)
			next = append(next, out[ins.offset:]...)
			out = next
		}
		if _, perr := goparser.ParseFile(token.NewFileSet(), f, out, 0); perr != nil {
			return count, parseErrorf("keyed-literal rewrite would produce a malformed file %s: %v", f, perr)
		}
		if err := os.WriteFile(f, out, 0644); err != nil {
			return count, err
		}
		if err := orchestrator.FormatImports(f); err != nil {
			fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", f, err)
		}
	}
	count = len(lits)
	return count, nil
}
