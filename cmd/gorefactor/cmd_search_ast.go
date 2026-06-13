package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

var searchASTFlags = map[string]bool{"--json": false, "--in": true}

func init() {
	registerCommand(Command{
		Name:        "search-ast",
		Description: "Structural search: match a Go statement/expression pattern, $_ is a wildcard [--in path] [--json]",
		Usage:       "search-ast '<pattern>' [--in path] [--json]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       searchASTFlags,
		Run:         searchASTCommand,
	})
}

// wildcardIdent is the internal identifier the user-facing `$_` wildcard is
// rewritten to before parsing (Go identifiers cannot contain `$`).
const wildcardIdent = "__gorefactor_wildcard__"

// astMatch is one structural match.
type astMatch struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

func searchASTCommand(args []string) error {
	positional, flags := parseFlags(args, searchASTFlags)
	pattern := positional[0]
	root := "."
	if flags["--in"] != "" {
		root = flags["--in"]
	}

	patExpr, patStmts, err := parseSearchPattern(pattern)
	if err != nil {
		return err
	}

	files, err := collectGoFiles(root, analyzer.DefaultWalkOptions())
	if err != nil {
		return err
	}

	var matches []astMatch
	for _, file := range files {
		matches = append(matches, searchFileAST(file, patExpr, patStmts)...)
	}

	if flags["--json"] != "" {
		emitJSON(map[string]interface{}{
			"pattern": pattern,
			"matches": matches,
			"total":   len(matches),
		})
		return nil
	}
	for _, m := range matches {
		fmt.Printf("%s:%d  %s\n", m.File, m.Line, m.Snippet)
	}
	fmt.Printf("%d match(es)\n", len(matches))
	return nil
}

// parseSearchPattern parses the pattern as an expression first, then as one
// or more statements. `$_` is rewritten to a wildcard identifier beforehand.
func parseSearchPattern(pattern string) (ast.Expr, []ast.Stmt, error) {
	rewritten := strings.ReplaceAll(pattern, "$_", wildcardIdent)
	if expr, err := parser.ParseExpr(rewritten); err == nil {
		return expr, nil, nil
	}
	src := "package p\nfunc _() {\n" + rewritten + "\n}"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "pattern.go", src, 0)
	if err != nil {
		return nil, nil, parseErrorf("pattern does not parse as a Go expression or statement: %s", pattern)
	}
	body := f.Decls[0].(*ast.FuncDecl).Body
	if len(body.List) == 0 {
		return nil, nil, parseErrorf("empty pattern")
	}
	return nil, body.List, nil
}

// searchFileAST returns all matches of the pattern in one file.
func searchFileAST(file string, patExpr ast.Expr, patStmts []ast.Stmt) []astMatch {
	src, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		return nil // sensors are best-effort on unparseable files
	}
	lines := strings.Split(string(src), "\n")
	snippetAt := func(pos token.Pos) astMatch {
		p := fset.Position(pos)
		snippet := ""
		if p.Line-1 < len(lines) {
			snippet = strings.TrimSpace(lines[p.Line-1])
		}
		return astMatch{File: file, Line: p.Line, Snippet: snippet}
	}

	var out []astMatch
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
	case reflect.Ptr:
		if pv.IsNil() || nv.IsNil() {
			return pv.IsNil() == nv.IsNil()
		}
		return matchASTNodes(pv.Elem().Interface(), nv.Elem().Interface())
	case reflect.Interface:
		if pv.IsNil() || nv.IsNil() {
			return pv.IsNil() == nv.IsNil()
		}
		return matchASTNodes(pv.Elem().Interface(), nv.Elem().Interface())
	case reflect.Struct:
		for i := 0; i < pv.NumField(); i++ {
			ft := pv.Type().Field(i)
			if ft.Type == reflect.TypeOf(token.Pos(0)) {
				continue // positions are irrelevant to structure
			}
			switch ft.Name {
			case "Obj", "Doc", "Comment": // identity/comment metadata
				continue
			}
			if !matchASTNodes(pv.Field(i).Interface(), nv.Field(i).Interface()) {
				return false
			}
		}
		return true
	case reflect.Slice:
		if pv.Len() != nv.Len() {
			return false
		}
		for i := 0; i < pv.Len(); i++ {
			if !matchASTNodes(pv.Index(i).Interface(), nv.Index(i).Interface()) {
				return false
			}
		}
		return true
	default:
		return pv.Interface() == nv.Interface()
	}
}
