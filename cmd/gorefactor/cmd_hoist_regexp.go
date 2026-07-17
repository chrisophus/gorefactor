package main

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/chrisophus/gorefactor/orchestrator"
)

func init() {
	registerCommand(Command{
		Name:        "hoist-regexp",
		Description: "Hoist function-local regexp.MustCompile calls with constant patterns to package-level vars (the regexp-compile-in-func autofix). Optionally scoped to one function.",
		Usage:       "hoist-regexp <file.go> [Func]",
		MinArgs:     1,
		MaxArgs:     2,
		Run:         hoistRegexpCommand,
	})
}

func hoistRegexpCommand(args []string) error {
	file := args[0]
	funcName := ""
	if len(args) > 1 {
		funcName = args[1]
	}
	n, err := hoistRegexpInFile(file, funcName)
	if err != nil {
		return err
	}
	if n == 0 {
		fmt.Println("hoist-regexp: no function-local literal regexp.MustCompile found")
		return nil
	}
	fmt.Printf("hoist-regexp: hoisted %d pattern(s) to package-level vars in %s\n", n, file)
	return nil
}

// hoistRegexpInFile rewrites every function-local regexp.MustCompile call with
// a literal pattern (optionally only inside funcName) into a reference to a
// new package-level var declared after the imports. MustCompile panics on a
// bad pattern either way, so moving the compile to package init is
// behavior-preserving; regexp.Compile is never touched (its error return
// would move).
func hoistRegexpInFile(path, funcName string) (int, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", path, err)
	}
	taken := fileScopeNames(astFile)
	offsetOf := func(p token.Pos) int { return fset.Position(p).Offset }

	// Byte-range edits keep every comment and all surrounding formatting exactly
	// where it was — reprinting the whole AST drags comments into the moved
	// nodes. Call sites shrink to the var name; the var decls are inserted as
	// text after the imports.
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	var varDecls []string

	for _, decl := range astFile.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil || fd.Name.Name == "init" {
			continue
		}
		if funcName != "" && fd.Name.Name != funcName {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, fn, ok := regexpCompileCall(n)
			if !ok || fn != "MustCompile" {
				return true
			}
			name := freshHoistName(fd.Name.Name, taken)
			taken[name] = true
			patSrc := string(src[offsetOf(call.Args[0].Pos()):offsetOf(call.Args[0].End())])
			varDecls = append(varDecls, fmt.Sprintf("var %s = regexp.MustCompile(%s)", name, patSrc))
			edits = append(edits, edit{start: offsetOf(call.Pos()), end: offsetOf(call.End()), text: name})
			return true
		})
	}
	if len(varDecls) == 0 {
		return 0, nil
	}

	insertOff := offsetOf(astFile.Name.End())
	for _, decl := range astFile.Decls {
		if g, ok := decl.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			insertOff = offsetOf(g.End())
		}
	}
	edits = append(edits, edit{start: insertOff, end: insertOff, text: "\n\n" + strings.Join(varDecls, "\n")})

	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := src
	for _, e := range edits {
		out = append(out[:e.start], append([]byte(e.text), out[e.end:]...)...)
	}
	formatted, err := format.Source(out)
	if err != nil {
		return 0, fmt.Errorf("gofmt after hoist: %w", err)
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		return 0, fmt.Errorf("write %s: %w", path, err)
	}
	return len(varDecls), orchestrator.FormatImports(path)

}

// fileScopeNames collects every top-level name declared in the file, so
// hoisted var names stay unique at least file-locally (the build/test verify
// gate catches the rare cross-file package collision).
func fileScopeNames(f *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			names[d.Name.Name] = true
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.ValueSpec:
					for _, id := range s.Names {
						names[id.Name] = true
					}
				case *ast.TypeSpec:
					names[s.Name.Name] = true
				}
			}
		}
	}
	return names
}

// freshHoistName derives a package-var name from the enclosing function
// ("parseHunk" -> "parseHunkRe", then "parseHunkRe2", ...).
func freshHoistName(funcName string, taken map[string]bool) string {
	base := funcName
	if base != "" {
		r := []rune(base)
		r[0] = unicode.ToLower(r[0])
		base = string(r)
	}
	base += "Re"
	name := base
	for i := 2; taken[name]; i++ {
		name = fmt.Sprintf("%s%d", base, i)
	}
	return name
}
