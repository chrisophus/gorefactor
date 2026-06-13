package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

var callgraphFlags = map[string]bool{
	"--json":    false,
	"--callers": false,
	"--callees": false,
	"--depth":   true,
	"--in":      true,
}

func init() {
	registerCommand(Command{
		Name:        "callgraph",
		Description: "Transitive call tree for a function/method (default: callees, depth 2) [--callers] [--depth N] [--json]",
		Usage:       "callgraph <Func|Receiver:Method> [--depth N] [--callers|--callees] [--in path] [--json]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       callgraphFlags,
		Run:         callgraphCommand,
	})
}

// cgNode is one function in the rendered call tree.
type cgNode struct {
	Name     string    `json:"name"`
	Receiver string    `json:"receiver,omitempty"`
	File     string    `json:"file"`
	Line     int       `json:"line"`
	Cycle    bool      `json:"cycle,omitempty"`
	Children []*cgNode `json:"children,omitempty"`
}

// cgDef is a function/method declaration found while indexing.
type cgDef struct {
	name     string
	receiver string
	file     string
	line     int
}

func (d *cgDef) key() string {
	if d.receiver != "" {
		return d.receiver + ":" + d.name
	}
	return d.name
}

// cgIndex holds the bidirectional call-edge index for a file set.
type cgIndex struct {
	defs    map[string]*cgDef   // key -> def
	callees map[string][]*cgDef // caller key -> called defs
	callers map[string][]*cgDef // callee key -> calling defs
}

func callgraphCommand(args []string) error {
	positional, flags := parseFlags(args, callgraphFlags)
	target := positional[0]
	root := "."
	if flags["--in"] != "" {
		root = flags["--in"]
	}
	depth := 2
	if flags["--depth"] != "" {
		n, err := strconv.Atoi(flags["--depth"])
		if err != nil || n < 1 {
			return usageErrorf("--depth requires a positive integer")
		}
		depth = n
	}
	if flags["--callers"] != "" && flags["--callees"] != "" {
		return usageErrorf("--callers and --callees are mutually exclusive")
	}
	direction := "callees"
	if flags["--callers"] != "" {
		direction = "callers"
	}

	files, err := collectGoFiles(root, analyzer.DefaultWalkOptions())
	if err != nil {
		return err
	}
	idx, err := buildCallIndex(files)
	if err != nil {
		return err
	}

	name, recv := splitNameReceiver(target)
	def := idx.lookup(name, recv)
	if def == nil {
		keys := make([]string, 0, len(idx.defs))
		for k := range idx.defs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 30 {
			keys = keys[:30]
		}
		return notFoundError(fmt.Sprintf("function %q not found", target), target, keys)
	}

	tree := idx.buildTree(def, direction, depth, map[string]bool{def.key(): true})

	if flags["--json"] != "" {
		emitJSON(map[string]interface{}{
			"target":    def.key(),
			"direction": direction,
			"depth":     depth,
			"tree":      tree,
		})
		return nil
	}
	fmt.Printf("%s of %s (depth %d):\n", direction, def.key(), depth)
	printCgNode(tree, 0)
	return nil
}

func printCgNode(n *cgNode, indent int) {
	label := n.Name
	if n.Receiver != "" {
		label = n.Receiver + ":" + n.Name
	}
	suffix := ""
	if n.Cycle {
		suffix = " [cycle]"
	}
	fmt.Printf("%s%s  %s:%d%s\n", strings.Repeat("  ", indent), label, n.File, n.Line, suffix)
	for _, c := range n.Children {
		printCgNode(c, indent+1)
	}
}

// lookup finds a definition by name and optional receiver. A bare name first
// matches a plain function, then falls back to a unique method of that name.
func (idx *cgIndex) lookup(name, recv string) *cgDef {
	if recv != "" {
		return idx.defs[recv+":"+name]
	}
	if d, ok := idx.defs[name]; ok {
		return d
	}
	var found *cgDef
	for _, d := range idx.defs {
		if d.name == name {
			if found != nil {
				return nil // ambiguous: require Receiver:Method
			}
			found = d
		}
	}
	return found
}

// buildTree renders the call tree in the requested direction. visited holds
// the keys on the current root-to-node path, so revisits are marked [cycle]
// and not expanded further.
func (idx *cgIndex) buildTree(def *cgDef, direction string, depth int, visited map[string]bool) *cgNode {
	node := &cgNode{Name: def.name, Receiver: def.receiver, File: def.file, Line: def.line}
	if depth == 0 {
		return node
	}
	next := idx.callees[def.key()]
	if direction == "callers" {
		next = idx.callers[def.key()]
	}
	for _, child := range next {
		if visited[child.key()] {
			node.Children = append(node.Children, &cgNode{
				Name: child.name, Receiver: child.receiver,
				File: child.file, Line: child.line, Cycle: true,
			})
			continue
		}
		visited[child.key()] = true
		node.Children = append(node.Children, idx.buildTree(child, direction, depth-1, visited))
		delete(visited, child.key())
	}
	return node
}

// buildCallIndex parses every file once and records call edges between
// declared functions. Selector calls (x.Foo()) are matched by method name;
// ident calls (Foo()) are matched against plain functions.
func buildCallIndex(files []string) (*cgIndex, error) {
	idx := &cgIndex{
		defs:    map[string]*cgDef{},
		callees: map[string][]*cgDef{},
		callers: map[string][]*cgDef{},
	}
	fset := token.NewFileSet()
	type rawCall struct {
		caller   *cgDef
		name     string
		selector bool
	}
	var calls []rawCall

	for _, file := range files {
		astFile, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			continue // skip unparseable files; sensors are best-effort
		}
		for _, decl := range astFile.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			def := &cgDef{
				name:     fn.Name.Name,
				receiver: cgReceiver(fn),
				file:     file,
				line:     fset.Position(fn.Pos()).Line,
			}
			if _, exists := idx.defs[def.key()]; !exists {
				idx.defs[def.key()] = def
			}
			caller := idx.defs[def.key()]
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch f := call.Fun.(type) {
				case *ast.Ident:
					calls = append(calls, rawCall{caller, f.Name, false})
				case *ast.SelectorExpr:
					calls = append(calls, rawCall{caller, f.Sel.Name, true})
				}
				return true
			})
		}
	}

	// Resolve raw calls against the definition index.
	seen := map[string]bool{} // "callerKey->calleeKey" dedupe
	for _, c := range calls {
		for _, callee := range resolveCallee(idx, c.name, c.selector) {
			edge := c.caller.key() + "->" + callee.key()
			if seen[edge] {
				continue
			}
			seen[edge] = true
			idx.callees[c.caller.key()] = append(idx.callees[c.caller.key()], callee)
			idx.callers[callee.key()] = append(idx.callers[callee.key()], c.caller)
		}
	}
	for _, m := range []map[string][]*cgDef{idx.callees, idx.callers} {
		for k := range m {
			sort.Slice(m[k], func(i, j int) bool { return m[k][i].key() < m[k][j].key() })
		}
	}
	return idx, nil
}

// resolveCallee maps a called name to candidate definitions. Without full
// type information, selector calls match methods of any receiver plus plain
// functions (package-qualified calls); ident calls match plain functions.
func resolveCallee(idx *cgIndex, name string, selector bool) []*cgDef {
	var out []*cgDef
	if d, ok := idx.defs[name]; ok {
		out = append(out, d)
	}
	if selector {
		for k, d := range idx.defs {
			if d.name == name && strings.Contains(k, ":") {
				out = append(out, d)
			}
		}
	}
	return out
}

func cgReceiver(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	switch tt := t.(type) {
	case *ast.Ident:
		return tt.Name
	case *ast.IndexExpr:
		if id, ok := tt.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.IndexListExpr:
		if id, ok := tt.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}
