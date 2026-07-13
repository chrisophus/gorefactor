package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"strings"

	"golang.org/x/tools/go/packages"
)

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

	// reserved names that cannot be param field names in the test struct.
	reserved := map[string]bool{"name": true, "wantErr": true}
	for i, r := range results {
		_ = r
		wn := fmt.Sprintf("want%d", i+1)
		if i == 0 && len(results) <= 2 {
			wn = "want"
		}
		reserved[wn] = true
	}
	// Map param names to safe struct field names.
	paramFields := make([]string, len(params))
	for i, p := range params {
		fn := p.name
		if reserved[fn] {
			fn = "in" + strings.ToUpper(p.name[:1]) + p.name[1:]
		}
		paramFields[i] = fn
	}

	fmt.Fprintf(&buf, "func %s(t *testing.T) {\n", testName)
	fmt.Fprintf(&buf, "\tcases := []struct {\n")
	fmt.Fprintf(&buf, "\t\tname string\n")
	for i, p := range params {
		fmt.Fprintf(&buf, "\t\t%s %s\n", paramFields[i], p.typStr)
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
	for i := range params {
		callArgs[i] = "tc." + paramFields[i]
	}
	callExpr := fn.Name.Name + "(" + strings.Join(callArgs, ", ") + ")"
	if recv != "" {
		// Need an instance — use zero value.
		callExpr = fmt.Sprintf("(%s{}).%s(%s)", recv, fn.Name.Name, strings.Join(callArgs, ", "))
	}

	extractBlockL82(results, buf, callExpr)

	fmt.Fprintf(&buf, "\t\t})\n")
	fmt.Fprintf(&buf, "\t}\n")
	fmt.Fprintf(&buf, "}\n")

	return buf.String(), testName, nil
}

func extractBlockL82(results []fieldInfo, buf bytes.Buffer, callExpr string) {
	if len(results) == 0 {
		fmt.Fprintf(&buf, "\t\t\t%s\n", callExpr)
	} else if len(results) == 1 && results[0].typStr == "error" {
		fmt.Fprintf(&buf, "\t\t\terr := %s\n", callExpr)
		fmt.Fprintf(&buf, "\t\t\tif (err != nil) != tc.wantErr {\n")
		fmt.Fprintf(&buf, "\t\t\t\tt.Errorf(\"got err %%v, wantErr %%v\", err, tc.wantErr)\n")
		fmt.Fprintf(&buf, "\t\t\t}\n")
	} else {

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
}
