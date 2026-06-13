package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
	goparser "go/parser"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"unicode"

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

	// Verify the scaffold itself parses before writing.
	if _, parseErr := goparser.ParseFile(token.NewFileSet(), "test.go", scaffold, 0); parseErr != nil {
		return m.fail(parseErrorf("generated scaffold does not parse: %v", parseErr))
	}

	var newContent string
	if _, statErr := os.Stat(testFile); os.IsNotExist(statErr) {
		// Create new test file.
		header := fmt.Sprintf("package %s\n\nimport \"testing\"\n\n", pkg.Name)
		if needsTarget(scaffold) {
			header = fmt.Sprintf("package %s\n\nimport (\n\t\"testing\"\n)\n\n", pkg.Name)
		}
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

// buildTestScaffold generates the table-driven test function source.
func buildTestScaffold(pkg *packages.Package, fn *ast.FuncDecl, recv, target string) (string, string, error) {
	testName := "Test" + fn.Name.Name
	if recv != "" {
		testName = "Test" + recv + "_" + fn.Name.Name
	}

	// Collect params and results.
	params := funcFields(fn.Type.Params)
	results := funcFields(fn.Type.Results)

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "func %s(t *testing.T) {\n", testName)
	fmt.Fprintf(&buf, "\tcases := []struct {\n")
	fmt.Fprintf(&buf, "\t\tname string\n")
	for _, p := range params {
		fmt.Fprintf(&buf, "\t\t%s %s\n", p.name, p.typStr)
	}
	if len(results) > 0 {
		// Only include non-error want fields.
		for i, r := range results {
			if r.typStr == "error" {
				fmt.Fprintf(&buf, "\t\twantErr bool\n")
			} else {
				wantName := fmt.Sprintf("want%d", i+1)
				if i == 0 && len(results) <= 2 {
					wantName = "want"
				}
				fmt.Fprintf(&buf, "\t\t%s %s\n", wantName, r.typStr)
			}
		}
	}
	fmt.Fprintf(&buf, "\t}{\n")
	fmt.Fprintf(&buf, "\t\t// TODO: add test cases\n")
	fmt.Fprintf(&buf, "\t}\n\n")
	fmt.Fprintf(&buf, "\tfor _, tc := range cases {\n")
	fmt.Fprintf(&buf, "\t\tt.Run(tc.name, func(t *testing.T) {\n")

	// Build the call expression.
	callArgs := make([]string, len(params))
	for i, p := range params {
		callArgs[i] = "tc." + p.name
	}
	callExpr := fn.Name.Name + "(" + strings.Join(callArgs, ", ") + ")"
	if recv != "" {
		// Need an instance — use zero value.
		callExpr = fmt.Sprintf("(%s{}).%s(%s)", recv, fn.Name.Name, strings.Join(callArgs, ", "))
	}

	if len(results) == 0 {
		fmt.Fprintf(&buf, "\t\t\t%s\n", callExpr)
	} else if len(results) == 1 && results[0].typStr == "error" {
		fmt.Fprintf(&buf, "\t\t\terr := %s\n", callExpr)
		fmt.Fprintf(&buf, "\t\t\tif (err != nil) != tc.wantErr {\n")
		fmt.Fprintf(&buf, "\t\t\t\tt.Errorf(\"got err %%v, wantErr %%v\", err, tc.wantErr)\n")
		fmt.Fprintf(&buf, "\t\t\t}\n")
	} else {
		// Multi-return.
		retVars := make([]string, len(results))
		for i, r := range results {
			if r.typStr == "error" {
				retVars[i] = "err"
			} else {
				retVars[i] = fmt.Sprintf("got%d", i+1)
				if i == 0 && len(results) <= 2 {
					retVars[i] = "got"
				}
			}
		}
		fmt.Fprintf(&buf, "\t\t\t%s := %s\n", strings.Join(retVars, ", "), callExpr)
		for i, r := range results {
			if r.typStr == "error" {
				fmt.Fprintf(&buf, "\t\t\tif (err != nil) != tc.wantErr {\n")
				fmt.Fprintf(&buf, "\t\t\t\tt.Errorf(\"got err %%v, wantErr %%v\", err, tc.wantErr)\n")
				fmt.Fprintf(&buf, "\t\t\t}\n")
			} else {
				got := retVars[i]
				want := fmt.Sprintf("want%d", i+1)
				if i == 0 && len(results) <= 2 {
					want = "want"
				}
				fmt.Fprintf(&buf, "\t\t\tif got, want := %s, tc.%s; got != want {\n", got, want)
				fmt.Fprintf(&buf, "\t\t\t\tt.Errorf(\"got %%v, want %%v\", got, want)\n")
				fmt.Fprintf(&buf, "\t\t\t}\n")
			}
		}
	}

	fmt.Fprintf(&buf, "\t\t})\n")
	fmt.Fprintf(&buf, "\t}\n")
	fmt.Fprintf(&buf, "}\n")

	return buf.String(), testName, nil
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

func zeroValueFor(typ types.Type) string {
	switch t := typ.Underlying().(type) {
	case *types.Basic:
		switch t.Kind() {
		case types.Bool:
			return "false"
		case types.String:
			return `""`
		default:
			if t.Info()&types.IsNumeric != 0 {
				return "0"
			}
		}
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Interface, *types.Signature:
		return "nil"
	}
	return fmt.Sprintf("%s{}", typ)
}

func needsTarget(scaffold string) bool {
	return strings.Contains(scaffold, "testing.")
}

// camelToLower converts CamelCase to a lowercase identifier.
func camelToLower(s string) string {
	var out []rune
	for i, r := range s {
		if i == 0 {
			out = append(out, unicode.ToLower(r))
		} else {
			out = append(out, r)
		}
	}
	return string(out)
}

var _ = camelToLower // used in template generation
var _ = zeroValueFor
