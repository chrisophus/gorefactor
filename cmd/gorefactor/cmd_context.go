package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

var contextFlags = map[string]bool{"--json": false, "--in": true, "--budget": true}

const defaultContextBudget = 4000

func init() {
	registerCommand(Command{
		Name:        "context",
		ReadOnly:    true,
		MCPTool:     true,
		Description: "One-shot LLM context pack for a symbol: definition, callers, signature types, tests [--budget N] [--json]",
		Usage:       "context <Symbol|Receiver:Method> [--budget N] [--in path] [--json]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       contextFlags,
		Run:         contextCommand,
	})
}

// contextCaller is one calling site with surrounding lines.
type contextCaller struct {
	Name      string `json:"name"`
	Signature string `json:"signature,omitempty"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Context   string `json:"context"` // calling line with 2 lines of context
}

// contextTypeDef is the definition of a type used in the symbol's signature.
type contextTypeDef struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Source string `json:"source"`
}

type contextPack struct {
	Symbol     string           `json:"symbol"`
	File       string           `json:"file"`
	Line       int              `json:"line"`
	Doc        string           `json:"doc,omitempty"`
	Definition string           `json:"definition"`
	Callers    []contextCaller  `json:"callers,omitempty"`
	Types      []contextTypeDef `json:"types,omitempty"`
	Tests      []string         `json:"tests,omitempty"`
	Budget     int              `json:"budget"`
	Truncated  bool             `json:"truncated,omitempty"`
	Notes      []string         `json:"notes,omitempty"`
}

func contextCommand(args []string) error {
	positional, flags := parseFlags(args, contextFlags)
	target := positional[0]
	root := "."
	if flags["--in"] != "" {
		root = flags["--in"]
	}
	budget := defaultContextBudget
	if flags["--budget"] != "" {
		n, err := strconv.Atoi(flags["--budget"])
		if err != nil || n < 200 {
			return usageErrorf("--budget requires an integer >= 200")
		}
		budget = n
	}

	pack, err := buildContextPack(target, root, budget)
	if err != nil {
		return err
	}
	if flags["--json"] != "" {
		emitJSON(pack)
		return nil
	}
	fmt.Print(renderContextPack(pack))
	return nil
}

func trimContextPack(pack *contextPack, budget int) {
	over := func() int { return len(renderContextPack(pack)) - budget }
	if over() <= 0 {
		return
	}
	pack.Truncated = true
	if n := len(pack.Types); n > 0 {
		for len(pack.Types) > 0 && over() > 0 {
			pack.Types = pack.Types[:len(pack.Types)-1]
		}
		if dropped := n - len(pack.Types); dropped > 0 {
			pack.Notes = append(pack.Notes, fmt.Sprintf("%d type(s) omitted (budget)", dropped))
		}
	}
	if n := len(pack.Callers); n > 0 && over() > 0 {
		for len(pack.Callers) > 0 && over() > 0 {
			pack.Callers = pack.Callers[:len(pack.Callers)-1]
		}
		if dropped := n - len(pack.Callers); dropped > 0 {
			pack.Notes = append(pack.Notes, fmt.Sprintf("%d caller(s) omitted (budget)", dropped))
		}
	}
	if over() > 0 {
		pack.Notes = append(pack.Notes, "definition truncated (budget)")
		const marker = "\n… [definition truncated: budget]"
		if d := over() + len(marker); len(pack.Definition) > d {
			cut := len(pack.Definition) - d
			for cut > 0 && !utf8.RuneStart(pack.Definition[cut]) {
				cut--
			}
			pack.Definition = pack.Definition[:cut] + marker
		}
	}
}

func renderContextPack(p *contextPack) string {
	var b strings.Builder
	fmt.Fprintf(&b, "── %s (%s:%d) ──\n", p.Symbol, p.File, p.Line)
	b.WriteString(p.Definition)
	if !strings.HasSuffix(p.Definition, "\n") {
		b.WriteString("\n")
	}
	if len(p.Callers) > 0 {
		fmt.Fprintf(&b, "\n── callers (%d) ──\n", len(p.Callers))
		for _, c := range p.Callers {
			fmt.Fprintf(&b, "%s:%d  %s\n", c.File, c.Line, c.Signature)
			b.WriteString(indentLines(c.Context, "  ") + "\n")
		}
	}
	if len(p.Types) > 0 {
		b.WriteString("\n── signature types ──\n")
		for _, t := range p.Types {
			fmt.Fprintf(&b, "// %s:%d\n%s\n", t.File, t.Line, t.Source)
		}
	}
	if len(p.Tests) > 0 {
		fmt.Fprintf(&b, "\n── tests ──\n%s\n", strings.Join(p.Tests, ", "))
	}
	if len(p.Notes) > 0 {
		fmt.Fprintf(&b, "\n[%s]\n", strings.Join(p.Notes, "; "))
	}
	return b.String()
}

func specHasName(spec ast.Spec, name string) bool {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		return s.Name.Name == name
	case *ast.ValueSpec:
		for _, n := range s.Names {
			if n.Name == name {
				return true
			}
		}
	}
	return false
}

// callerSignature returns the one-line signature of the calling function.
func callerSignature(file, name, recv string) string {
	src, err := os.ReadFile(file)
	if err != nil {
		return name
	}
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		return name
	}
	for _, decl := range astFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name {
			continue
		}
		if recv != "" && cgReceiver(fn) != strings.TrimPrefix(recv, "*") {
			continue
		}
		start := fset.Position(fn.Pos()).Offset
		end := fset.Position(fn.Type.End()).Offset
		return strings.Join(strings.Fields(string(src[start:end])), " ")
	}
	return name
}

// sourceContext returns the line at `line` with n lines of context each side.
func sourceContext(file string, line, n int) string {
	src, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(src), "\n")
	lo := line - 1 - n
	if lo < 0 {
		lo = 0
	}
	hi := line + n
	if hi > len(lines) {
		hi = len(lines)
	}
	var out []string
	for i := lo; i < hi; i++ {
		marker := "  "
		if i == line-1 {
			marker = "> "
		}
		out = append(out, fmt.Sprintf("%s%d: %s", marker, i+1, strings.TrimRight(lines[i], " \t")))
	}
	return strings.Join(out, "\n")
}

// signatureTypeNames collects the named, non-builtin types mentioned in a
// function signature (parameters and results), without duplicates.
func signatureTypeNames(ft *ast.FuncType) []string {
	builtins := map[string]bool{
		"bool": true, "string": true, "error": true, "any": true,
		"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
		"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true, "uintptr": true,
		"byte": true, "rune": true, "float32": true, "float64": true,
		"complex64": true, "complex128": true,
	}
	seen := map[string]bool{}
	var out []string
	collect := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			ast.Inspect(f.Type, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					_ = sel // qualified types live in other packages; skip lookup
					return false
				}
				if id, ok := n.(*ast.Ident); ok && !builtins[id.Name] && !seen[id.Name] {
					seen[id.Name] = true
					out = append(out, id.Name)
				}
				return true
			})
		}
	}
	collect(ft.Params)
	collect(ft.Results)
	return out
}

func indentLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}
