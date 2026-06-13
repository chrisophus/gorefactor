package main

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

	"github.com/chrisophus/gorefactor/analyzer"
)

var apiDiffFlags = map[string]bool{"--json": false}

func init() {
	registerCommand(Command{
		Name:        "api-diff",
		Description: "Compare the exported API surface of the working tree against a git ref (default HEAD) [--json]",
		Usage:       "api-diff [git-ref] [--json]",
		MinArgs:     0,
		MaxArgs:     1,
		Flags:       apiDiffFlags,
		Run:         apiDiffCommand,
	})
}

// apiChange is one signature change between the ref and the working tree.
type apiChange struct {
	Symbol string `json:"symbol"`
	Old    string `json:"old"`
	New    string `json:"new"`
}

type apiDiffResult struct {
	Ref      string      `json:"ref"`
	Added    []string    `json:"added"`
	Removed  []string    `json:"removed"`
	Changed  []apiChange `json:"changed"`
	Breaking bool        `json:"breaking"`
}

func apiDiffCommand(args []string) error {
	positional, flags := parseFlags(args, apiDiffFlags)
	ref := "HEAD"
	if len(positional) > 0 {
		ref = positional[0]
	}

	res, err := computeAPIDiff(ref)
	if err != nil {
		return err
	}
	if flags["--json"] != "" {
		emitJSON(res)
		return nil
	}
	printAPIDiff(res)
	return nil
}

func printAPIDiff(res *apiDiffResult) {
	verdict := "no breaking changes"
	if res.Breaking {
		verdict = "BREAKING"
	}
	fmt.Printf("api-diff vs %s: %d added, %d removed, %d changed (%s)\n",
		res.Ref, len(res.Added), len(res.Removed), len(res.Changed), verdict)
	for _, s := range res.Added {
		fmt.Printf("+ %s\n", s)
	}
	for _, s := range res.Removed {
		fmt.Printf("- %s\n", s)
	}
	for _, c := range res.Changed {
		fmt.Printf("~ %s\n    old: %s\n    new: %s\n", c.Symbol, c.Old, c.New)
	}
}

// computeAPIDiff builds the exported-API map of both sides and diffs them.
// It is a sensor: always exit 0, the verdict lives in the breaking field.
func computeAPIDiff(ref string) (*apiDiffResult, error) {
	prefix, err := gitShowPrefix()
	if err != nil {
		return nil, fmt.Errorf("api-diff requires a git repository: %w", err)
	}

	newAPI := map[string]string{}
	oldAPI := map[string]string{}

	files, err := collectGoFiles(".", analyzer.DefaultWalkOptions())
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, rerr := os.ReadFile(f)
		if rerr != nil {
			continue
		}
		collectExportedAPI(filepath.ToSlash(f), src, newAPI)
	}

	for _, f := range gitGoFilesAt(ref, prefix) {
		src, gerr := gitFileAt(ref, prefix+f)
		if gerr != nil {
			continue
		}
		collectExportedAPI(f, src, oldAPI)
	}

	res := &apiDiffResult{Ref: ref, Added: []string{}, Removed: []string{}, Changed: []apiChange{}}
	for sym, sig := range newAPI {
		old, ok := oldAPI[sym]
		switch {
		case !ok:
			res.Added = append(res.Added, sym+" "+sig)
		case old != sig:
			res.Changed = append(res.Changed, apiChange{Symbol: sym, Old: old, New: sig})
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

func gitShowPrefix() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-prefix").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitGoFilesAt lists non-test .go files at ref under the current directory,
// returned relative to the current directory.
func gitGoFilesAt(ref, prefix string) []string {
	out, err := exec.Command("git", "ls-tree", "-r", "--name-only", "--full-tree", ref).Output()
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

func gitFileAt(ref, path string) ([]byte, error) {
	return exec.Command("git", "show", ref+":"+filepath.ToSlash(path)).Output()
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
		recv := cgReceiver(d)
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
