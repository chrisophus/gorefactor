package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
	"golang.org/x/tools/go/packages"
)

var changeSignatureFlags = mutFlagSpec(map[string]bool{
	"--add-param":      true,
	"--position":       true,
	"--call-value":     true,
	"--remove-param":   true,
	"--rename-param":   true,
	"--reorder-params": true,
	"--change-returns": true,
})

func init() {
	registerCommand(Command{
		Name:        "change-signature",
		Description: "Change a function/method signature and update all call sites (add/remove/rename a parameter)",
		Usage:       `change-signature <file> <Func|Receiver:Method> (--add-param "name type" [--position N] [--call-value EXPR] | --remove-param <name|index> | --rename-param <old> <new>) [--json] [--dry-run] [--gate]`,
		MinArgs:     2,
		MaxArgs:     3,
		Flags:       changeSignatureFlags,
		Run:         changeSignatureCommand,
	})
}

// sigAction is the parsed --add-param/--remove-param/--rename-param request.
type sigAction struct {
	kind      string // "add" | "remove" | "rename"
	paramName string
	paramType string
	position  int // -1 = append at end
	callValue string
	removeRef string // name or 0-based index
	oldName   string
	newName   string
}

func parseSignatureAction(flags map[string]string, pos []string) (*sigAction, error) {
	if flags["--reorder-params"] != "" {
		return nil, usageErrorf("--reorder-params is not supported (out of scope for change-signature)")
	}
	if flags["--change-returns"] != "" {
		return nil, usageErrorf("--change-returns is not supported (out of scope for change-signature)")
	}
	a := &sigAction{position: -1}
	count := 0
	if v := flags["--add-param"]; v != "" {
		count++
		a.kind = "add"
		fields := strings.Fields(v)
		if len(fields) < 2 {
			return nil, usageErrorf(`--add-param wants "name type" (e.g. --add-param "ctx context.Context")`)
		}
		a.paramName = fields[0]
		a.paramType = strings.Join(fields[1:], " ")
		if !token.IsIdentifier(a.paramName) {
			return nil, usageErrorf("invalid parameter name %q", a.paramName)
		}
	}
	if v := flags["--remove-param"]; v != "" {
		count++
		a.kind = "remove"
		a.removeRef = v
	}
	if v := flags["--rename-param"]; v != "" {
		count++
		a.kind = "rename"
		old, newName := v, ""
		for _, sep := range []string{"=", ",", " "} {
			if i := strings.Index(v, sep); i >= 0 {
				old, newName = v[:i], v[i+len(sep):]
				break
			}
		}
		if newName == "" && len(pos) >= 3 {
			newName = pos[2] // --rename-param old new (two-token form)
		}
		if old == "" || newName == "" {
			return nil, usageErrorf("--rename-param wants <old> <new> (or \"old=new\")")
		}
		if !token.IsIdentifier(newName) {
			return nil, usageErrorf("invalid parameter name %q", newName)
		}
		a.oldName, a.newName = strings.TrimSpace(old), strings.TrimSpace(newName)
	}
	if count != 1 {
		return nil, usageErrorf("change-signature wants exactly one of --add-param, --remove-param, --rename-param")
	}
	if v, ok := flags["--position"]; ok {
		if a.kind != "add" {
			return nil, usageErrorf("--position only applies to --add-param")
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, usageErrorf("--position wants a non-negative integer, got %q", v)
		}
		a.position = n
	}
	if v, ok := flags["--call-value"]; ok {
		if a.kind != "add" {
			return nil, usageErrorf("--call-value only applies to --add-param")
		}
		a.callValue = v
	}
	return a, nil
}

func changeSignatureCommand(args []string) error {
	pos, flags := parseFlags(args, changeSignatureFlags)
	if len(pos) < 2 {
		return usageErrorf("usage: change-signature <file> <Func|Receiver:Method> --add-param|--remove-param|--rename-param ...")
	}
	file, locator := pos[0], pos[1]
	m := &mutation{op: "change-signature", file: file}
	m.setCommonFlags(flags)

	action, err := parseSignatureAction(flags, pos)
	if err != nil {
		return m.fail(err)
	}
	loc, err := parseFuncLocator(locator)
	if err != nil {
		return m.fail(err)
	}
	if err := validateFuncTarget(file, loc); err != nil {
		return m.fail(err)
	}

	pkgs, absFile, err := loadTypedPackages(file, true)
	if err != nil {
		return m.fail(err)
	}
	if msgs := packagesErrors(pkgs); len(msgs) > 0 {
		return m.fail(parseErrorf("module does not type-check; fix these before changing signatures:\n  %s",
			strings.Join(msgs, "\n  ")))
	}
	tgt, err := locateSignatureTarget(pkgs, absFile, loc)
	if err != nil {
		return m.fail(err)
	}
	if tp := tgt.fn.Type.TypeParams; tp != nil && len(tp.List) > 0 {
		return m.fail(usageErrorf("change-signature does not support generic functions"))
	}

	edits, detail, err := buildSignatureEdits(pkgs, tgt, action, locator)
	if err != nil {
		return m.fail(err)
	}
	m.files = editFiles(edits)
	return m.run(func() (string, error) {
		if err := applyTextEdits(edits); err != nil {
			return "", err
		}
		return detail, nil
	})
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
			recvName := receiverTypeName(fn.Recv.List[0].Type)
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
	return nil, notFoundErrorf("function %q not found in any loaded package for %s", name, absFile)
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
			if receiverTypeName(fn.Recv.List[0].Type) == loc.ReceiverType {
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

// placeholder
