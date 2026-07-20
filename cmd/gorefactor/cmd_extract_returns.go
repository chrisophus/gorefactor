package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"sort"
	"strings"
)

// directReturns collects the return statements that belong to the extracted
// block itself. Returns inside function literals have their own activation
// frame and never escape the block, so they are not lifted (and are no reason
// to refuse extraction).
func directReturns(stmts []ast.Stmt) []*ast.ReturnStmt {
	var out []*ast.ReturnStmt
	for _, s := range stmts {
		ast.Inspect(s, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			if ret, ok := n.(*ast.ReturnStmt); ok {
				out = append(out, ret)
			}
			return true
		})
	}
	return out
}

// enclosingResultTypes renders the source text of each result type of fn, one
// entry per result value (multi-name fields are expanded).
func enclosingResultTypes(fset *token.FileSet, fn *ast.FuncDecl) ([]string, error) {
	if fn.Type.Results == nil {
		return nil, nil
	}
	var out []string
	for _, f := range fn.Type.Results.List {
		var buf bytes.Buffer
		if err := printer.Fprint(&buf, fset, f.Type); err != nil {
			return nil, fmt.Errorf("fprint: %w", err)
		}
		n := len(f.Names)
		if n == 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			out = append(out, buf.String())
		}
	}
	return out, nil
}

// validateReturnLift checks that lifting the block's return statements into a
// (results..., done bool) helper is a purely mechanical transform. It refuses
// the cases where the rewrite would need semantic judgment: blocks that also
// assign variables read after the block (the lifted returns and the
// write-backs would race for the return statement), naked returns when the
// enclosing function has results (their values live in the caller's named
// result variables, invisible to the helper), and single-call multi-value
// returns (appending `true` to `return f()` is not valid Go).
func validateReturnLift(fset *token.FileSet, rets []*ast.ReturnStmt, nResults int, writes []paramSpec) error {
	if len(writes) > 0 {
		var names []string
		for _, w := range writes {
			names = append(names, w.name)
		}
		return fmt.Errorf("block assigns variable(s) used after it (%s) and also contains return statements; extract a smaller block", strings.Join(names, ", "))
	}
	for _, ret := range rets {
		line := fset.Position(ret.Pos()).Line
		if nResults > 0 && len(ret.Results) == 0 {
			return fmt.Errorf("naked return at line %d relies on the enclosing function's named results, which a helper cannot see", line)
		}
		if nResults > 1 && len(ret.Results) == 1 {
			return fmt.Errorf("return at line %d forwards multiple values from a single call; expand it to explicit values first", line)
		}
	}
	return nil
}

// liftResultNames picks collision-free names for the helper's named results:
// r0..rN-1 for the lifted result values and done for the fell-through flag.
// Any identifier appearing in the block or the parameter list forces a
// numeric suffix bump so the generated names never capture existing ones.
func liftResultNames(stmts []ast.Stmt, params []paramSpec, nResults int) (resultNames []string, doneName string) {
	used := map[string]bool{}
	for _, p := range params {
		used[p.name] = true
	}
	for _, s := range stmts {
		ast.Inspect(s, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				used[id.Name] = true
			}
			return true
		})
	}
	pick := func(base string) string {
		name := base
		for i := 1; used[name]; i++ {
			name = fmt.Sprintf("%s%d", base, i)
		}
		used[name] = true
		return name
	}
	for i := 0; i < nResults; i++ {
		resultNames = append(resultNames, pick(fmt.Sprintf("r%d", i)))
	}
	return resultNames, pick("done")
}

// returnLiftSpec bundles the inputs for synthesizing a return-lifting helper: parsed positions, the
// block's statements, the inferred params/results, and the raw source bytes the body is spliced
// from.
type returnLiftSpec struct {
	fset        *token.FileSet
	methodName  string
	stmts       []ast.Stmt
	params      []paramSpec
	rets        []*ast.ReturnStmt
	resultTypes []string
	src         []byte
	isTail      bool
}

// blockIsFuncTail reports whether the extracted block ends the enclosing function body (its last
// statement is the function's last statement).
func blockIsFuncTail(blockStmts []ast.Stmt, fn *ast.FuncDecl) bool {
	if fn.Body == nil || len(fn.Body.List) == 0 || len(blockStmts) == 0 {
		return false
	}
	return fn.Body.List[len(fn.Body.List)-1] == blockStmts[len(blockStmts)-1]
}

// buildReturnLiftedFunc synthesizes the helper for a block that contains direct return statements.
// The helper's results are the enclosing function's result types plus a trailing done flag, all
// named so the final naked return yields zero values with done=false when the block falls through.
// Every direct return in the block gets `, true` appended (or becomes `return true` when the
// enclosing function has no results). The body is spliced from the original source bytes, so
// comments and formatting inside the block survive. The call site propagates a taken return:
//
// if r0, done := helper(args); done { return r0 }
func buildReturnLiftedFunc(spec returnLiftSpec) (newFunc, callSite string, err error) {
	fset, stmts, src := spec.fset, spec.stmts, spec.src
	methodName, params, rets, resultTypes, isTail := spec.methodName, spec.params, spec.rets, spec.resultTypes, spec.isTail
	startOff := fset.Position(stmts[0].Pos()).Offset
	endOff := fset.Position(stmts[len(stmts)-1].End()).Offset
	if startOff < 0 || endOff > len(src) || startOff >= endOff {
		return "", "", fmt.Errorf("block offset computation failed")
	}
	body := string(src[startOff:endOff])

	// Rewrite the direct returns inside the body text back-to-front so
	// earlier offsets stay valid.
	type retEdit struct {
		at   int
		text string
	}
	var edits []retEdit
	for _, ret := range rets {
		if len(ret.Results) > 0 {
			edits = append(edits, retEdit{at: fset.Position(ret.End()).Offset - startOff, text: ", true"})
		} else {
			edits = append(edits, retEdit{at: fset.Position(ret.Pos()).Offset + len("return") - startOff, text: " true"})
		}
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].at > edits[j].at })
	for _, e := range edits {
		if e.at < 0 || e.at > len(body) {
			return "", "", fmt.Errorf("return offset computation failed")
		}
		body = body[:e.at] + e.text + body[e.at:]
	}

	resultNames, doneName := liftResultNames(stmts, params, len(resultTypes))

	var sb strings.Builder
	sb.WriteString("\nfunc ")
	sb.WriteString(methodName)
	sb.WriteString("(")
	for i, p := range params {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s %s", p.name, p.typeS)
	}
	sb.WriteString(") (")
	for i, t := range resultTypes {
		fmt.Fprintf(&sb, "%s %s, ", resultNames[i], t)
	}
	fmt.Fprintf(&sb, "%s bool) {\n\t", doneName)
	sb.WriteString(body)
	sb.WriteString("\n\treturn\n}\n")

	var args []string
	for _, p := range params {
		args = append(args, p.name)
	}
	call := fmt.Sprintf("%s(%s)", methodName, strings.Join(args, ", "))
	switch {
	case len(resultTypes) == 0:
		// Void function: falling through after the block is legal, so a
		// conditional return suffices even at the tail.
		callSite = fmt.Sprintf("if %s {\n\treturn\n}", call)
	case isTail:
		// The block ended the function, so every path through it returned in
		// the original; done is always true. Bind the results and return them
		// unconditionally — a conditional here would fall off the end.
		lhs := strings.Join(append(append([]string(nil), resultNames...), "_"), ", ")
		callSite = fmt.Sprintf("%s := %s\n\treturn %s", lhs, call, strings.Join(resultNames, ", "))
	default:
		lhs := strings.Join(append(append([]string(nil), resultNames...), doneName), ", ")
		callSite = fmt.Sprintf("if %s := %s; %s {\n\treturn %s\n}", lhs, call, doneName, strings.Join(resultNames, ", "))
	}
	return sb.String(), callSite, nil
}
