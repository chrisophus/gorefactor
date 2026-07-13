package analyzer

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// APIChange is one signature change between a git ref and the working tree.
type APIChange struct {
	Symbol string `json:"symbol"`
	Old    string `json:"old"`
	New    string `json:"new"`
}

// APIDiffResult is the exported-API delta between a git ref and the working
// tree. It is a sensor result: Breaking carries the verdict, not an error.
type APIDiffResult struct {
	Ref      string      `json:"ref"`
	Added    []string    `json:"added"`
	Removed  []string    `json:"removed"`
	Changed  []APIChange `json:"changed"`
	Breaking bool        `json:"breaking"`
}

// ComputeAPIDiff builds the exported-API map of the working tree under dir and
// of the same tree at git ref, then diffs them. dir is the directory to run git
// in and to walk for current sources (use "." for the current working dir).
// Working-tree API keys are computed relative to dir so they line up with the
// git-ref keys (which are relative to the repo prefix of dir).
func ComputeAPIDiff(dir, ref string) (*APIDiffResult, error) {
	prefix, err := gitShowPrefix(dir)
	if err != nil {
		return nil, fmt.Errorf("api-diff requires a git repository: %w", err)
	}

	newAPI := map[string]string{}
	oldAPI := map[string]string{}

	files, err := WalkGoFiles(dir, DefaultWalkOptions())
	if err != nil {
		return nil, fmt.Errorf("walk go files: %w", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, rerr := os.ReadFile(f)
		if rerr != nil {
			continue
		}
		rel, rerr := filepath.Rel(dir, f)
		if rerr != nil {
			rel = f
		}
		collectExportedAPI(filepath.ToSlash(rel), src, newAPI)
	}

	for _, f := range gitGoFilesAt(dir, ref, prefix) {
		src, gerr := gitFileAt(dir, ref, prefix+f)
		if gerr != nil {
			continue
		}
		collectExportedAPI(f, src, oldAPI)
	}

	res := &APIDiffResult{Ref: ref, Added: []string{}, Removed: []string{}, Changed: []APIChange{}}
	for sym, sig := range newAPI {
		old, ok := oldAPI[sym]
		switch {
		case !ok:
			res.Added = append(res.Added, sym+" "+sig)
		case old != sig:
			res.Changed = append(res.Changed, APIChange{Symbol: sym, Old: old, New: sig})
		}
	}
	for sym, sig := range oldAPI {
		if _, ok := newAPI[sym]; !ok {
			res.Removed = append(res.Removed, sym+" "+sig)
		}
	}
	sort.Strings(res.Added)
	sort.Strings(res.Removed)
	sort.Slice(res.Changed, func(i, j int) bool { return res.Changed[i].Symbol < res.Changed[j].Symbol })
	res.Breaking = len(res.Removed) > 0 || len(res.Changed) > 0
	return res, nil
}

// FuncReceiverName returns the (unqualified) receiver type name of a method decl,
// stripping a leading pointer and any generic type parameters.
func FuncReceiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	switch tt := t.(type) {
	case *ast.Ident:
		return tt.Name
	case *ast.IndexExpr:
		if id, ok := tt.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.IndexListExpr:
		if id, ok := tt.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

func gitShowPrefix(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-prefix")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitGoFilesAt lists non-test .go files at ref under dir, returned relative to
// dir (the git prefix stripped).
func gitGoFilesAt(dir, ref, prefix string) []string {
	cmd := exec.Command("git", "ls-tree", "-r", "--name-only", "--full-tree", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if !strings.HasSuffix(line, ".go") || strings.HasSuffix(line, "_test.go") {
			continue
		}
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rel := strings.TrimPrefix(line, prefix)
		if strings.Contains(rel, "vendor/") || strings.HasPrefix(rel, ".") {
			continue
		}
		files = append(files, rel)
	}
	return files
}

func gitFileAt(dir, ref, path string) ([]byte, error) {
	cmd := exec.Command("git", "show", ref+":"+filepath.ToSlash(path))
	cmd.Dir = dir
	return cmd.Output()
}

// collectExportedAPI extracts the exported API surface of one file into api,
// keyed "pkgdir.Symbol" (functions/types/consts/vars), "pkgdir.Type.Method",
// "pkgdir.Type.Field" or "pkgdir.Iface.Method".
func collectExportedAPI(relPath string, src []byte, api map[string]string) {
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, relPath, src, 0)
	if err != nil {
		return // best effort: unparseable side contributes nothing
	}
	qualifier := filepath.ToSlash(filepath.Dir(relPath))
	if qualifier == "." {
		qualifier = astFile.Name.Name
	}
	render := func(n ast.Node) string {
		var b strings.Builder
		if err := format.Node(&b, fset, n); err != nil {
			return "?"
		}
		return strings.Join(strings.Fields(b.String()), " ")
	}

	for _, decl := range astFile.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			collectFuncAPI(d, qualifier, render, api)
		case *ast.GenDecl:
			collectGenAPI(d, qualifier, render, api)
		}
	}
}

func collectFuncAPI(d *ast.FuncDecl, qualifier string, render func(ast.Node) string, api map[string]string) {
	if !d.Name.IsExported() {
		return
	}
	if d.Recv != nil && len(d.Recv.List) > 0 {
		recv := FuncReceiverName(d)
		if recv == "" || !ast.IsExported(recv) {
			return
		}
		api[qualifier+"."+recv+"."+d.Name.Name] = render(d.Type)
		return
	}
	api[qualifier+"."+d.Name.Name] = render(d.Type)
}

func collectGenAPI(d *ast.GenDecl, qualifier string, render func(ast.Node) string, api map[string]string) {
	kind := strings.ToLower(d.Tok.String())
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			if !s.Name.IsExported() {
				continue
			}
			collectTypeAPI(s, qualifier, render, api)
		case *ast.ValueSpec:
			for _, n := range s.Names {
				if !n.IsExported() {
					continue
				}
				sig := kind
				if s.Type != nil {
					sig = kind + " " + render(s.Type)
				} else if len(s.Values) > 0 {
					sig = kind + " = " + render(s.Values[0])
				}
				api[qualifier+"."+n.Name] = sig
			}
		}
	}
}

func collectTypeAPI(s *ast.TypeSpec, qualifier string, render func(ast.Node) string, api map[string]string) {
	name := qualifier + "." + s.Name.Name
	switch t := s.Type.(type) {
	case *ast.StructType:
		api[name] = "struct"
		for _, f := range t.Fields.List {
			if len(f.Names) == 0 { // embedded field
				api[name+"."+render(f.Type)] = "embedded"
				continue
			}
			for _, fn := range f.Names {
				if fn.IsExported() {
					api[name+"."+fn.Name] = render(f.Type)
				}
			}
		}
	case *ast.InterfaceType:
		api[name] = "interface"
		for _, m := range t.Methods.List {
			if len(m.Names) == 0 {
				api[name+"."+render(m.Type)] = "embedded"
				continue
			}
			for _, mn := range m.Names {
				if mn.IsExported() {
					api[name+"."+mn.Name] = render(m.Type)
				}
			}
		}
	default:
		api[name] = render(s.Type)
	}
}
