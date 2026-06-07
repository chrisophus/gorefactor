package main

import (
	"bufio"
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

const defaultSplitMaxLines = 300
const defaultTestFileMaxLines = 1000

type splitDecl struct {
	name      string
	receiver  string
	isMethod  bool
	startLine int
	endLine   int
}

func (d splitDecl) lines() int { return d.endLine - d.startLine + 1 }

func (d splitDecl) targetName() string {
	if d.isMethod {
		return d.receiver + ":" + d.name
	}
	return d.name
}

type splitGroup struct {
	key   string
	decls []splitDecl
}

func (g splitGroup) totalLines() int {
	n := 0
	for _, d := range g.decls {
		n += d.lines()
	}
	return n
}

func parseSplitDecls(filePath string) ([]splitDecl, error) {
	fset := token.NewFileSet()
	node, err := goparser.ParseFile(fset, filePath, nil, 0)
	if err != nil {
		return nil, err
	}
	var out []splitDecl
	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		d := splitDecl{
			name:      fn.Name.Name,
			startLine: fset.Position(fn.Pos()).Line,
			endLine:   fset.Position(fn.End()).Line,
		}
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			d.isMethod = true
			d.receiver = receiverTypeName(fn.Recv.List[0].Type)
		}
		out = append(out, d)
	}
	return out, nil
}

func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}

func groupSplitDecls(decls []splitDecl) []splitGroup {
	groups := map[string]*splitGroup{}
	getOrCreate := func(key string) *splitGroup {
		if g, ok := groups[key]; ok {
			return g
		}
		g := &splitGroup{key: key}
		groups[key] = g
		return g
	}
	for _, d := range decls {
		switch {
		case d.isMethod && d.receiver != "":
			g := getOrCreate("recv:" + d.receiver)
			g.decls = append(g.decls, d)
		default:
			if prefix := commonPrefix(d.name); prefix != "" {
				g := getOrCreate("prefix:" + prefix)
				g.decls = append(g.decls, d)
			} else {
				g := getOrCreate("single:" + d.name)
				g.decls = append(g.decls, d)
			}
		}
	}
	out := make([]splitGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	return out
}

func commonPrefix(name string) string {
	if len(name) < 6 {
		return ""
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		isUpper := c >= 'A' && c <= 'Z'
		isDigit := c >= '0' && c <= '9'
		if isUpper || isDigit {
			if i >= 3 {
				return name[:i]
			}
			return ""
		}
	}
	return ""
}

func destFileFor(dir, stem string, g splitGroup, used map[string]bool) string {
	suffix := ""
	switch {
	case strings.HasPrefix(g.key, "recv:"):
		suffix = camelToSnake(strings.TrimPrefix(g.key, "recv:"))
	case strings.HasPrefix(g.key, "prefix:"):
		suffix = camelToSnake(strings.TrimPrefix(g.key, "prefix:"))
	case strings.HasPrefix(g.key, "single:"):
		suffix = camelToSnake(strings.TrimPrefix(g.key, "single:"))
	}
	if suffix == "" {
		suffix = "part"
	}
	base := stem + "_" + suffix
	candidate := base + ".go"
	if strings.HasSuffix(stem, "_test") {
		base = strings.TrimSuffix(stem, "_test") + "_" + suffix + "_test"
		candidate = base + ".go"
	}
	i := 2
	for used[candidate] {
		candidate = fmt.Sprintf("%s%d.go", base, i)
		i++
	}
	used[candidate] = true
	return filepath.Join(dir, candidate)
}

func camelToSnake(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func fileLineCount(filePath string) (int, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	n := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		n++
	}
	return n, scanner.Err()
}
