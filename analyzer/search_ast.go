package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
)

// wildcardIdent is the internal identifier the user-facing `$_` wildcard is
// rewritten to before parsing (Go identifiers cannot contain `$`).
const wildcardIdent = "__gorefactor_wildcard__"

// ASTMatch is one structural match of a search pattern.
type ASTMatch struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

// SearchASTInDir parses pattern once and returns all structural matches across
// the non-skipped Go files under dir. `$_` in the pattern is a wildcard.
func SearchASTInDir(dir, pattern string) ([]ASTMatch, error) {
	patExpr, patStmts, err := ParseSearchPattern(pattern)
	if err != nil {
		return nil, fmt.Errorf("parse search pattern: %w", err)
	}
	files, err := WalkGoFiles(dir, DefaultWalkOptions())
	if err != nil {
		return nil, fmt.Errorf("walk go files: %w", err)
	}
	var matches []ASTMatch
	for _, file := range files {
		matches = append(matches, SearchFileAST(file, patExpr, patStmts)...)
	}
	return matches, nil
}

// ParseSearchPattern parses the pattern as an expression first, then as one or
// more statements. `$_` is rewritten to a wildcard identifier beforehand.
func ParseSearchPattern(pattern string) (ast.Expr, []ast.Stmt, error) {
	rewritten := strings.ReplaceAll(pattern, "$_", wildcardIdent)
	if expr, err := parser.ParseExpr(rewritten); err == nil {
		return expr, nil, nil
	}
	src := "package p\nfunc _() {\n" + rewritten + "\n}"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "pattern.go", src, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("pattern does not parse as a Go expression or statement: %s", pattern)
	}
	body := f.Decls[0].(*ast.FuncDecl).Body
	if len(body.List) == 0 {
		return nil, nil, fmt.Errorf("empty pattern")
	}
	return nil, body.List, nil
}

// SearchFileAST returns all matches of the pattern in one file. Unparseable
// files contribute nothing (structural search is best-effort).
func SearchFileAST(file string, patExpr ast.Expr, patStmts []ast.Stmt) []ASTMatch {
	src, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(src), "\n")
	snippetAt := func(pos token.Pos) ASTMatch {
		p := fset.Position(pos)
		snippet := ""
		if p.Line-1 < len(lines) {
			snippet = strings.TrimSpace(lines[p.Line-1])
		}
		return ASTMatch{File: file, Line: p.Line, Snippet: snippet}
	}

	var out []ASTMatch
	ast.Inspect(astFile, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		switch {
		case patExpr != nil:
			if e, ok := n.(ast.Expr); ok && matchASTNodes(patExpr, e) {
				out = append(out, snippetAt(n.Pos()))
			}
		case len(patStmts) == 1:
			if s, ok := n.(ast.Stmt); ok && matchASTNodes(patStmts[0], s) {
				out = append(out, snippetAt(n.Pos()))
			}
		default:
			for _, window := range stmtListsOf(n) {
				for i := 0; i+len(patStmts) <= len(window); i++ {
					if matchStmtSeq(patStmts, window[i:i+len(patStmts)]) {
						out = append(out, snippetAt(window[i].Pos()))
					}
				}
			}
		}
		return true
	})
	return out
}

func stmtListsOf(n ast.Node) [][]ast.Stmt {
	switch b := n.(type) {
	case *ast.BlockStmt:
		return [][]ast.Stmt{b.List}
	case *ast.CaseClause:
		return [][]ast.Stmt{b.Body}
	case *ast.CommClause:
		return [][]ast.Stmt{b.Body}
	}
	return nil
}

func matchStmtSeq(pat, stmts []ast.Stmt) bool {
	for i := range pat {
		if !matchASTNodes(pat[i], stmts[i]) {
			return false
		}
	}
	return true
}

// matchASTNodes structurally compares a pattern fragment against a candidate
// node, ignoring positions and comments. A wildcard identifier in the pattern
// matches any single expression.
func matchASTNodes(pat, node interface{}) bool {
	if id, ok := pat.(*ast.Ident); ok && id != nil && id.Name == wildcardIdent {
		expr, isExpr := node.(ast.Expr)
		return isExpr && expr != nil && !reflect.ValueOf(expr).IsNil()
	}
	pv := reflect.ValueOf(pat)
	nv := reflect.ValueOf(node)
	if !pv.IsValid() || !nv.IsValid() {
		return pv.IsValid() == nv.IsValid()
	}
	if pv.Type() != nv.Type() {
		return false
	}
	switch pv.Kind() {
	case reflect.Pointer, reflect.Interface:
		return matchASTIndirect(pv, nv)
	case reflect.Struct:
		return matchASTStruct(pv, nv)
	case reflect.Slice:
		return matchASTSlice(pv, nv)
	default:
		return pv.Interface() == nv.Interface()
	}

}

func matchASTIndirect(pv, nv reflect.Value) bool {
	if pv.IsNil() || nv.IsNil() {
		return pv.IsNil() == nv.IsNil()
	}
	return matchASTNodes(pv.Elem().Interface(), nv.Elem().Interface())
}

func astSkipField(name string) bool {
	switch name {
	case "Obj", "Doc", "Comment":
		return true
	}
	return false
}

func matchASTStruct(pv, nv reflect.Value) bool {
	for i := 0; i < pv.NumField(); i++ {
		ft := pv.Type().Field(i)
		if ft.Type == reflect.TypeOf(token.Pos(0)) || astSkipField(ft.Name) {
			continue
		}
		if !matchASTNodes(pv.Field(i).Interface(), nv.Field(i).Interface()) {
			return false
		}
	}
	return true
}

func matchASTSlice(pv, nv reflect.Value) bool {
	if pv.Len() != nv.Len() {
		return false
	}
	for i := 0; i < pv.Len(); i++ {
		if !matchASTNodes(pv.Index(i).Interface(), nv.Index(i).Interface()) {
			return false
		}
	}
	return true
}
