package orchestrator

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/tools/go/packages"
)

// renameDeclarationTyped performs a go/types object-identity rename of the
// package-level symbol oldName to newName. It rewrites only the ast.Ident nodes
// whose resolved types.Object is the same declared object, so — unlike a
// name-string rewrite — it never touches shadowing locals, same-named struct
// fields, or unrelated identifiers, and it follows the object across every file
// in the package (cross-file rename). Exported symbols are handled by the caller
// (they can be referenced from packages this call does not load).
func renameDeclarationTyped(file, oldName, newName string, result *OperationResult) error {
	absFile, err := filepath.Abs(file)
	if err != nil {
		return err
	}
	pkgs, err := loadPackagesForRename(absFile)
	if err != nil {
		return err
	}

	// file -> offset -> ident length. Keying by byte offset dedups the same
	// textual location discovered through multiple package variants (e.g. the
	// _test.go re-check of the same source file).
	edits := map[string]map[int]int{}
	targetDir := filepath.Dir(absFile)
	var candidates []string
	seenCand := map[string]bool{}
	matched := false

	for _, p := range pkgs {
		if p.Types == nil || p.TypesInfo == nil {
			continue
		}
		if !packageInDir(p, targetDir) {
			continue
		}
		scope := p.Types.Scope()
		for _, n := range scope.Names() {
			if !seenCand[n] {
				seenCand[n] = true
				candidates = append(candidates, n)
			}
		}
		obj := scope.Lookup(oldName)
		if obj == nil {
			continue
		}
		// Only require the package we actually edit to type-check; a partial
		// type-check would make info.Uses incomplete and the rename unsound.
		if perr := firstTypeError(p); perr != "" {
			return fmt.Errorf("package does not type-check; fix before renaming: %s", perr)
		}
		matched = true
		collectIdentEdits(p, obj, edits)
	}

	if !matched {
		sort.Strings(candidates)
		return fmt.Errorf("symbol %q not found as a package-level declaration in %s; available: %v",
			oldName, targetDir, candidates)
	}
	if len(edits) == 0 {
		return fmt.Errorf("symbol %q resolved but produced no identifiers to rename", oldName)
	}
	return applyRenameEdits(edits, oldName, newName, result)
}

// loadPackagesForRename loads the package containing absFile (plus its _test.go
// variants) with full syntax and type information.
func loadPackagesForRename(absFile string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedCompiledGoFiles |
			packages.NeedDeps | packages.NeedImports,
		Dir:   filepath.Dir(absFile),
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, fmt.Errorf("load packages for rename: %w", err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no Go packages found for %s", absFile)
	}
	return pkgs, nil
}

// packageInDir reports whether any of p's source files live directly in dir.
func packageInDir(p *packages.Package, dir string) bool {
	for _, group := range [][]string{p.GoFiles, p.CompiledGoFiles} {
		for _, f := range group {
			if abs, err := filepath.Abs(f); err == nil && filepath.Dir(abs) == dir {
				return true
			}
		}
	}
	return false
}

// firstTypeError returns the first load/parse/type error attached to p, or "".
func firstTypeError(p *packages.Package) string {
	if len(p.Errors) > 0 {
		return p.Errors[0].Error()
	}
	return ""
}

// collectIdentEdits records the byte range of every ast.Ident in p that resolves
// (via Defs or Uses) to obj.
func collectIdentEdits(p *packages.Package, obj types.Object, edits map[string]map[int]int) {
	fset := p.Fset
	record := func(id *ast.Ident) {
		pos := fset.Position(id.Pos())
		if pos.Filename == "" {
			return
		}
		abs, err := filepath.Abs(pos.Filename)
		if err != nil {
			abs = pos.Filename
		}
		m := edits[abs]
		if m == nil {
			m = map[int]int{}
			edits[abs] = m
		}
		m[pos.Offset] = len(id.Name)
	}
	for id, o := range p.TypesInfo.Defs {
		if o == obj {
			record(id)
		}
	}
	for id, o := range p.TypesInfo.Uses {
		if o == obj {
			record(id)
		}
	}
}

// applyRenameEdits rewrites the collected identifier ranges to newName, parse
// checks each file before writing, then repairs imports.
func applyRenameEdits(edits map[string]map[int]int, oldName, newName string, result *OperationResult) error {
	files := make([]string, 0, len(edits))
	for f := range edits {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, file := range files {
		offsets := edits[file]
		keys := make([]int, 0, len(offsets))
		for off := range offsets {
			keys = append(keys, off)
		}
		// Descending so each replacement leaves earlier offsets valid.
		sort.Sort(sort.Reverse(sort.IntSlice(keys)))
		src, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		for _, off := range keys {
			length := offsets[off]
			if off < 0 || off+length > len(src) {
				return fmt.Errorf("rename edit out of range in %s (%d+%d > %d)", file, off, length, len(src))
			}
			src = append(src[:off], append([]byte(newName), src[off+length:]...)...)
		}
		fset := token.NewFileSet()
		if _, perr := goparser.ParseFile(fset, file, src, 0); perr != nil {
			return fmt.Errorf("internal: rename of %s does not parse, refusing to write: %v", file, perr)
		}
		if err := os.WriteFile(file, src, 0644); err != nil {
			return err
		}
		if err := formatImports(file); err != nil {
			fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
		}
		result.Changes = append(result.Changes, &CodeChange{
			Type:        "rename_declaration",
			File:        file,
			Description: fmt.Sprintf("Renamed %q to %q in %s (%d occurrence(s))", oldName, newName, file, len(offsets)),
		})
	}
	return nil
}
