// Package changesig is the change-signature engine: it adds, removes, or renames
// a parameter on a function/method and rewrites every call site across the
// module. It is importable by the CLI, the MCP server, and other library callers
// without pulling in package main.
package changesig

import (
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"strings"

	"github.com/chrisophus/gorefactor/internal/cerr"
	"github.com/chrisophus/gorefactor/internal/goload"
	"github.com/chrisophus/gorefactor/orchestrator"

	"golang.org/x/tools/go/packages"
)

// Action is the parsed --add-param/--remove-param/--rename-param request. The
// CLI wrapper builds it from flags; the engine consumes it in Plan.
type Action struct {
	Kind      string // "add" | "remove" | "rename"
	ParamName string
	ParamType string
	Position  int // -1 = append at end
	CallValue string
	RemoveRef string // name or 0-based index
	OldName   string
	NewName   string
}

// Plan loads and type-checks the module containing file, locates the target
// function/method named by locator, and computes the text edits (plus a
// human-readable detail string) that apply action to it and every call site.
// It performs the same refusals the CLI relies on (missing target, generic
// functions, unsafe call sites, interface satisfaction) as classified errors.
func Plan(file, locator string, action *Action) ([]TextEdit, string, error) {
	loc, err := goload.ParseFuncLocator(locator)
	if err != nil {
		return nil, "", fmt.Errorf("parse func locator: %w", err)
	}
	if err := goload.ValidateFuncTarget(file, loc); err != nil {
		return nil, "", fmt.Errorf("validate func target: %w", err)
	}
	pkgs, absFile, err := goload.LoadTypedPackages(file, true)
	if err != nil {
		return nil, "", fmt.Errorf("load typed packages: %w", err)
	}
	if msgs := goload.PackagesErrors(pkgs); len(msgs) > 0 {
		return nil, "", cerr.Parsef("module does not type-check; fix these before changing signatures:\n  %s",
			strings.Join(msgs, "\n  "))
	}
	tgt, err := locateSignatureTarget(pkgs, absFile, loc)
	if err != nil {
		return nil, "", fmt.Errorf("locate signature target: %w", err)
	}
	if tp := tgt.fn.Type.TypeParams; tp != nil && len(tp.List) > 0 {
		return nil, "", cerr.Usagef("change-signature does not support generic functions")
	}
	return buildSignatureEdits(pkgs, tgt, action, locator)
}

// sigTarget bundles the located declaration with its package context.
type sigTarget struct {
	pkg  *packages.Package
	fn   *ast.FuncDecl
	recv *types.Named // non-nil for methods
}

func locateSignatureTarget(pkgs []*packages.Package, absFile string, loc *orchestrator.InsertionLocation) (*sigTarget, error) {
	var best *sigTarget
	for _, p := range pkgs {
		fileAST := syntaxFileIn(p, absFile)
		if fileAST == nil {
			continue
		}
		fn := findFuncDeclByLocator(fileAST, loc)
		if fn == nil {
			continue
		}
		t := &sigTarget{pkg: p, fn: fn}
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			recvName := goload.ReceiverTypeName(fn.Recv.List[0].Type)
			if obj, ok := p.Types.Scope().Lookup(recvName).(*types.TypeName); ok {
				if named, ok := obj.Type().(*types.Named); ok {
					t.recv = named
				}
			}
		}
		if p.ID == p.PkgPath {
			return t, nil
		}
		if best == nil {
			best = t
		}
	}
	if best != nil {
		return best, nil
	}
	name := loc.FunctionName
	if name == "" {
		name = loc.ReceiverType + ":" + loc.MethodName
	}
	return nil, cerr.NotFoundf("function %q not found in any loaded package for %s", name, absFile)
}

func syntaxFileIn(p *packages.Package, absFile string) *ast.File {
	for _, f := range p.Syntax {
		pos := p.Fset.Position(f.Pos())
		if abs, err := filepath.Abs(pos.Filename); err == nil && abs == absFile {
			return f
		}
	}
	return nil
}

func findFuncDeclByLocator(fileAST *ast.File, loc *orchestrator.InsertionLocation) *ast.FuncDecl {
	for _, decl := range fileAST.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if loc.MethodName != "" {
			if fn.Recv == nil || len(fn.Recv.List) == 0 || fn.Name.Name != loc.MethodName {
				continue
			}
			if goload.ReceiverTypeName(fn.Recv.List[0].Type) == loc.ReceiverType {
				return fn
			}
			continue
		}
		if fn.Recv == nil && fn.Name.Name == loc.FunctionName {
			return fn
		}
	}
	return nil
}
