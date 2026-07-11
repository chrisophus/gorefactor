package analyzer

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
)

// LogReturnFixSite identifies one autofixable log-propagation finding. Line
// and Column anchor the return statement of the pattern — the same position
// the detector rules report — so lint can match fixable sites to issues.
type LogReturnFixSite struct {
	Rule     string
	Line     int
	Column   int
	Function string
}

// srcEdit is a byte-range replacement on the original source. Edits never
// overlap and are applied back-to-front so earlier offsets stay valid.
type srcEdit struct {
	start, end int
	repl       string
}

// ApplyLogReturnFixes rewrites the autofixable subset of the log-propagation
// findings in src:
//
//   - wrap-log-return / wrap-bridge-log-return: the log statement between the
//     fmt.Errorf wrap and the return is deleted (the wrap already carries the
//     context; the caller decides whether to log).
//   - if-err-log-return: a log statement immediately followed by the flagged
//     return inside an `if err != nil` body is deleted; a bare `return err`
//     is additionally wrapped with fmt.Errorf("<context>: %w", err).
//
// Non-adjacent log/return findings are left alone by default — deleting a log
// that also serves other code paths is not a single safe transform. With
// aggressive set, a log followed by the flagged return with other statements
// between them (same statement list of an `if err != nil` body) is fixed too;
// callers must gate that on build+test verification. rule limits fixing to
// one detector ("" fixes all three). Returns nil output when nothing matched.
func ApplyLogReturnFixes(filename string, src []byte, rule string, aggressive bool) ([]byte, []LogReturnFixSite, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", filename, err)
	}
	edits, sites := collectLogReturnEdits(f, fset, src, rule, aggressive)
	if len(edits) == 0 {
		return nil, nil, nil
	}
	return applySrcEdits(src, edits), sites, nil

}

// ListLogReturnFixSites reports the sites ApplyLogReturnFixes would fix in
// file, without modifying anything. Lint rules use it to attach an autofix
// only to issues the fixer can actually resolve.
func ListLogReturnFixSites(file string, aggressive bool) ([]LogReturnFixSite, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	_, sites := collectLogReturnEdits(f, fset, src, "", aggressive)
	return sites, nil

}

func collectLogReturnEdits(f *ast.File, fset *token.FileSet, src []byte, rule string, aggressive bool) ([]srcEdit, []LogReturnFixSite) {
	want := func(r string) bool { return rule == "" || rule == r }
	var edits []srcEdit
	var sites []LogReturnFixSite
	deletedLogs := make(map[ast.Stmt]bool)
	wrappedRets := make(map[*ast.ReturnStmt]bool)

	record := func(r string, fn *ast.FuncDecl, ret *ast.ReturnStmt) {
		pos := fset.Position(ret.Pos())
		sites = append(sites, LogReturnFixSite{Rule: r, Line: pos.Line, Column: pos.Column, Function: fn.Name.Name})
	}

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		// Runs before the if-err-log-return pass so that pass never re-claims a
		// log statement that belongs to a triple or quad.
		edits = append(edits, collectWrapQuadEdits(fn, fset, src, want, deletedLogs, record)...)
		if !want("if-err-log-return") {
			continue
		}
		edits = append(edits, collectIfErrLogReturnEdits(fn, fset, src, aggressive, deletedLogs, wrappedRets, record)...)
	}
	return edits, sites

}

func collectWrapQuadEdits(fn *ast.FuncDecl, fset *token.FileSet, src []byte, want func(string) bool, deletedLogs map[ast.Stmt]bool, record func(string, *ast.FuncDecl, *ast.ReturnStmt)) []srcEdit {
	var edits []srcEdit
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		blk, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		list := blk.List
		for i := 0; i+2 < len(list); i++ {
			if want("wrap-log-return") && isAssignErrFmtWrap(list[i]) && isStructuredLogStmt(list[i+1]) {
				if ret, ok := list[i+2].(*ast.ReturnStmt); ok && returnLastIsBareErr(ret) && !deletedLogs[list[i+1]] {
					deletedLogs[list[i+1]] = true
					edits = append(edits, deleteStmtEdit(fset, src, list[i+1]))
					record("wrap-log-return", fn, ret)
				}
			}
			if want("wrap-bridge-log-return") {
				if ret, ok := wrapBridgeLogReturnQuadAt(list, i); ok && !deletedLogs[list[i+2]] {
					deletedLogs[list[i+2]] = true
					edits = append(edits, deleteStmtEdit(fset, src, list[i+2]))
					record("wrap-bridge-log-return", fn, ret)
				}
			}
		}
		return true
	})
	return edits
}

func collectIfErrLogReturnEdits(fn *ast.FuncDecl, fset *token.FileSet, src []byte, aggressive bool, deletedLogs map[ast.Stmt]bool, wrappedRets map[*ast.ReturnStmt]bool, record func(string, *ast.FuncDecl, *ast.ReturnStmt)) []srcEdit {
	var edits []srcEdit
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok || !isErrNotNil(ifStmt.Cond) {
			return true
		}
		list := ifStmt.Body.List
		for i := 0; i+1 < len(list); i++ {
			if !isStructuredLogStmt(list[i]) || deletedLogs[list[i]] {
				continue
			}

			if i > 0 && isAssignErrFmtWrap(list[i-1]) {
				continue
			}
			j := i + 1
			if aggressive {
				for j < len(list) {
					if _, isRet := list[j].(*ast.ReturnStmt); isRet {
						break
					}
					j++
				}
				if j >= len(list) {
					continue
				}
			}
			ret, ok := list[j].(*ast.ReturnStmt)
			if !ok {
				continue
			}
			switch {
			case isBareReturnErr(ret):
				deletedLogs[list[i]] = true
				edits = append(edits, deleteStmtEdit(fset, src, list[i]))

				if !wrappedRets[ret] {
					wrappedRets[ret] = true
					edits = append(edits, wrapReturnErrEdit(fset, ret, logReturnWrapContext(ifStmt, fn)))
				}
				record("if-err-log-return", fn, ret)
			case isReturnFmtErrorfWrappingErr(ret):
				deletedLogs[list[i]] = true
				edits = append(edits, deleteStmtEdit(fset, src, list[i]))
				record("if-err-log-return", fn, ret)
			}
		}
		return true
	})
	return edits
}

// logReturnWrapContext derives the fmt.Errorf context for a wrapped return:
// the called function's name from `if err := call(); err != nil`, falling
// back to the enclosing function's name, lower-cased into words.
func logReturnWrapContext(ifStmt *ast.IfStmt, fn *ast.FuncDecl) string {
	if ifStmt.Init != nil {
		if as, ok := ifStmt.Init.(*ast.AssignStmt); ok && len(as.Rhs) == 1 {
			if call, ok := as.Rhs[0].(*ast.CallExpr); ok {
				if name := calledName(call.Fun); name != "" {
					return camelWords(name)
				}
			}
		}
	}
	return camelWords(fn.Name.Name)
}

func calledName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// camelWords converts CamelCase to lower-case words: "FetchUser" → "fetch user".
func camelWords(name string) string {
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'A' && c <= 'Z' {
			if i > 0 && (name[i-1] < 'A' || name[i-1] > 'Z') {
				b.WriteByte(' ')
			}
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

// wrapReturnErrEdit replaces the bare `err` result of ret (guaranteed by
// isBareReturnErr) with fmt.Errorf("<context>: %w", err).
func wrapReturnErrEdit(fset *token.FileSet, ret *ast.ReturnStmt, context string) srcEdit {
	id := ret.Results[0]
	start := fset.Position(id.Pos()).Offset
	end := fset.Position(id.End()).Offset
	return srcEdit{start: start, end: end, repl: fmt.Sprintf("fmt.Errorf(%q, err)", context+": %w")}
}

// deleteStmtEdit removes a statement including its line when the statement
// is alone on it: leading indentation and the trailing newline go with it.
func deleteStmtEdit(fset *token.FileSet, src []byte, s ast.Stmt) srcEdit {
	start := fset.Position(s.Pos()).Offset
	end := fset.Position(s.End()).Offset
	lineStart := start
	for lineStart > 0 && src[lineStart-1] != '\n' {
		lineStart--
	}
	if len(bytes.TrimSpace(src[lineStart:start])) == 0 {
		start = lineStart
	}
	for end < len(src) && (src[end] == ' ' || src[end] == '\t' || src[end] == '\r') {
		end++
	}
	if end < len(src) && src[end] == '\n' {
		end++
	}
	return srcEdit{start: start, end: end}
}

func applySrcEdits(src []byte, edits []srcEdit) []byte {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := append([]byte(nil), src...)
	for _, e := range edits {
		out = append(out[:e.start], append([]byte(e.repl), out[e.end:]...)...)
	}
	return out
}
