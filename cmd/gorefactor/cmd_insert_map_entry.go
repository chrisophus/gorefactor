package main

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"strings"
)

var insertMapEntryFlags = mutFlagSpec(nil)

func init() {
	registerCommand(Command{
		Name:        "insert-map-entry",
		Mutates:     true,
		Description: "Append an element to a composite literal (a package-level var, or the literal returned by a func)",
		Usage:       "insert-map-entry <file> <VarOrFunc> <element|-> [--json] [--dry-run] [--gate]",
		MinArgs:     2,
		MaxArgs:     3,
		Flags:       insertMapEntryFlags,
		Run:         insertMapEntryCommand,
	})
}

// insertMapEntryCommand appends a new element to a composite literal. The
// target names either a package-level var bound to a composite literal
// (map/slice/struct) or a function whose body contains one (e.g. a catalog
// builder that returns a slice literal). The element is the raw text of the
// new entry — "key: value" for a map, or an element expression for a slice.
// It is spliced after the last element and the whole file is re-parsed and
// gofmt'd, so malformed input is rejected rather than written.
func insertMapEntryCommand(args []string) error {
	pos, flags := parseFlags(args, insertMapEntryFlags)
	if len(pos) < 2 {
		return usageErrorf("usage: insert-map-entry <file> <VarOrFunc> <element> (else stdin)")
	}
	file, target := pos[0], pos[1]

	m := &mutation{op: "insert-map-entry", file: file}
	m.setCommonFlags(flags)

	element, err := readContentArg(pos, 2)
	if err != nil {
		return m.fail(err)
	}
	element = strings.TrimSpace(element)
	// Tolerate a trailing comma in the element — a model naturally writes
	// `"key": true,` and the command supplies its own separator, so keeping
	// it would produce a double comma and a malformed file.
	element = strings.TrimSpace(strings.TrimSuffix(element, ","))
	if element == "" {
		return m.fail(usageErrorf("element must be non-empty"))
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

	lit := findCompositeLit(node, target)
	if lit == nil {
		return m.fail(notFoundErrorf("no composite literal found for %q in %s (expected a package-level var or a func returning one)", target, file))
	}

	// Insert after the last element (with a separating comma), or just after
	// the opening brace when the literal is empty.
	var insertOff int
	var insertText string
	if len(lit.Elts) == 0 {
		insertOff = fset.Position(lit.Lbrace).Offset + 1
		insertText = element
	} else {
		last := lit.Elts[len(lit.Elts)-1]
		insertOff = fset.Position(last.End()).Offset
		insertText = ",\n" + element
	}
	if insertOff < 0 || insertOff > len(src) {
		return m.fail(fmt.Errorf("could not determine composite-literal insertion offset"))
	}

	out := append([]byte{}, src[:insertOff]...)
	out = append(out, []byte(insertText)...)
	out = append(out, src[insertOff:]...)

	return m.validateAndWrite(file, out, "inserting the element",
		fmt.Sprintf("Appended an element to %s in %s", target, file))
}

// findCompositeLit locates the composite literal to extend. It first looks for
// a package-level var named target bound to a composite literal; failing that,
// for a func named target and the first composite literal in its body.
func findCompositeLit(node *ast.File, target string) *ast.CompositeLit {
	for _, d := range node.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name != target || i >= len(vs.Values) {
					continue
				}
				if cl, ok := vs.Values[i].(*ast.CompositeLit); ok {
					return cl
				}
			}
		}
	}
	for _, d := range node.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != target || fd.Body == nil {
			continue
		}
		found := firstNodeOf[*ast.CompositeLit](fd.Body)
		return found
	}
	return nil
}
