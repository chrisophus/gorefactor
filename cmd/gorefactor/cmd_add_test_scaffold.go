package main

import (
	"bytes"
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
	"golang.org/x/tools/go/packages"
)

var addTestFlags = mutFlagSpec(nil)

func init() {
	registerCommand(Command{
		Name:        "add-test",
		Description: "Generate a table-driven test scaffold for an exported function or method",
		Usage:       "add-test <file> <Func|Receiver:Method> [--json] [--dry-run]",
		MinArgs:     2,
		MaxArgs:     2,
		Flags:       addTestFlags,
		Run:         addTestCommand,
	})
}

func addTestCommand(args []string) error {
	pos, flags := parseFlags(args, addTestFlags)
	if len(pos) < 2 {
		return usageErrorf("usage: add-test <file> <Func|Receiver:Method>")
	}
	file, target := pos[0], pos[1]
	m := &mutation{op: "add-test", file: file}
	m.setCommonFlags(flags)

	pkgs, absFile, err := loadTypedPackages(file, false)
	if err != nil {
		return m.fail(err)
	}
	pkg, astFile := findFileInPackages(pkgs, absFile)
	if pkg == nil {
		return m.fail(notFoundErrorf("file %s not in any loaded package", file))
	}

	fn, recv, err := findFuncForTest(pkg, astFile, target)
	if err != nil {
		return m.fail(err)
	}

	scaffold, testFuncName, err := buildTestScaffold(pkg, fn, recv, target)
	if err != nil {
		return m.fail(err)
	}

	// Determine _test.go path.
	base := strings.TrimSuffix(filepath.Base(file), ".go")
	testFile := filepath.Join(filepath.Dir(file), base+"_test.go")

	var newContent string
	if _, statErr := os.Stat(testFile); os.IsNotExist(statErr) {
		// Create new test file.
		header := fmt.Sprintf("package %s\n\nimport \"testing\"\n\n", pkg.Name)
		newContent = header + scaffold
	} else {
		// Append to existing test file.
		existing, readErr := os.ReadFile(testFile)
		if readErr != nil {
			return m.fail(readErr)
		}
		if strings.Contains(string(existing), "func "+testFuncName+"(") {
			return m.fail(notFoundErrorf("test %s already exists in %s", testFuncName, testFile))
		}
		newContent = strings.TrimRight(string(existing), "\n") + "\n\n" + scaffold
	}

	// Verify the full content parses before writing.
	if _, parseErr := goparser.ParseFile(token.NewFileSet(), testFile, newContent, 0); parseErr != nil {
		return m.fail(parseErrorf("generated test does not parse: %v", parseErr))
	}

	m.file = testFile
	m.files = []string{testFile}

	return m.run(func() (string, error) {
		if err := os.WriteFile(testFile, []byte(newContent), 0644); err != nil {
			return "", err
		}
		// Format via goimports.
		_ = orchestrator.FormatImports(testFile)
		return fmt.Sprintf("added %s to %s", testFuncName, testFile), nil
	})
}

// findFuncForTest locates the target function/method in the AST.
func findFuncForTest(pkg *packages.Package, astFile *ast.File, target string) (*ast.FuncDecl, string, error) {
	recvName, methodName, isMethod := strings.Cut(target, ":")

	var candidates []string
	var found *ast.FuncDecl
	var foundRecv string

	for _, decl := range astFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if isMethod {
			if fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			rv := addTestReceiverTypeName(fn.Recv.List[0].Type)
			if rv != recvName {
				continue
			}
			candidates = append(candidates, rv+":"+fn.Name.Name)
			if fn.Name.Name == methodName {
				found = fn
				foundRecv = rv
			}
		} else {
			if fn.Recv != nil {
				continue
			}
			candidates = append(candidates, fn.Name.Name)
			if fn.Name.Name == target {
				found = fn
			}
		}
	}

	if found == nil {
		return nil, "", notFoundErrorf("function %q not found in %s\navailable: %s",
			target, astFile.Name.Name, strings.Join(candidates, ", "))
	}
	_ = pkg
	return found, foundRecv, nil
}

type fieldInfo struct {
	name   string
	typStr string
}

func funcFields(fl *ast.FieldList) []fieldInfo {
	if fl == nil {
		return nil
	}
	var out []fieldInfo
	idx := 0
	for _, f := range fl.List {
		ts := typeString(f.Type)
		if len(f.Names) == 0 {
			name := fmt.Sprintf("arg%d", idx)
			if ts == "error" {
				name = "err"
			}
			out = append(out, fieldInfo{name: name, typStr: ts})
			idx++
		}
		for _, n := range f.Names {
			nm := n.Name
			if nm == "_" || nm == "" {
				nm = fmt.Sprintf("arg%d", idx)
			}
			out = append(out, fieldInfo{name: nm, typStr: ts})
			idx++
		}
	}
	return out
}

func typeString(expr ast.Expr) string {
	var buf bytes.Buffer
	writeTypeExpr(&buf, expr)
	return buf.String()
}

func writeTypeExpr(buf *bytes.Buffer, expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.Ident:
		buf.WriteString(e.Name)
	case *ast.SelectorExpr:
		writeTypeExpr(buf, e.X)
		buf.WriteByte('.')
		buf.WriteString(e.Sel.Name)
	case *ast.StarExpr:
		buf.WriteByte('*')
		writeTypeExpr(buf, e.X)
	case *ast.ArrayType:
		buf.WriteString("[]")
		writeTypeExpr(buf, e.Elt)
	case *ast.MapType:
		buf.WriteString("map[")
		writeTypeExpr(buf, e.Key)
		buf.WriteByte(']')
		writeTypeExpr(buf, e.Value)
	case *ast.InterfaceType:
		buf.WriteString("interface{}")
	case *ast.Ellipsis:
		buf.WriteString("...")
		writeTypeExpr(buf, e.Elt)
	default:
		buf.WriteString("interface{}")
	}
}

func addTestReceiverTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return addTestReceiverTypeName(e.X)
	case *ast.IndexExpr:
		return addTestReceiverTypeName(e.X)
	}
	return ""
}

var _ = camelToLower // used in template generation
var _ = zeroValueFor
