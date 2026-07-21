package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

var skeletonFlags = map[string]bool{"--json": false}

func init() {
	registerCommand(Command{
		Name:        "skeleton",
		ReadOnly:    true,
		MCPTool:     true,
		Description: "Print a file with function bodies elided — token-cheap file shape [--json]",
		Usage:       "skeleton <file.go> [--json]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       skeletonFlags,
		Run:         skeletonCommand,
	})
}

// skeletonDecl is one entry in the --json outline.
type skeletonDecl struct {
	Kind      string `json:"kind"` // func, method, type, const, var, import
	Name      string `json:"name,omitempty"`
	Receiver  string `json:"receiver,omitempty"`
	Signature string `json:"signature,omitempty"`
	Doc       string `json:"doc,omitempty"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	BodyLines int    `json:"bodyLines,omitempty"`
}

type skeletonOutline struct {
	File    string         `json:"file"`
	Package string         `json:"package"`
	Decls   []skeletonDecl `json:"decls"`
}

func skeletonCommand(args []string) error {
	positional, flags := parseFlags(args, skeletonFlags)
	file := positional[0]

	src, err := os.ReadFile(file)
	if err != nil {
		return notFoundErrorf("cannot read %s: %v", file, err)
	}
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, file, src, parser.ParseComments)
	if err != nil {
		return parseErrorf("%s does not parse: %v", file, err)
	}

	if flags["--json"] != "" {
		emitEnvelope(true, "", buildSkeletonOutline(file, fset, astFile, src))

		return nil
	}
	fmt.Print(renderSkeleton(fset, astFile, src))
	return nil
}

// renderSkeleton prints the source with each function body replaced by
// "{ /* N lines */ }". Everything else (package clause, imports, types,
// consts, vars, doc comments, signatures) is kept verbatim.
func renderSkeleton(fset *token.FileSet, astFile *ast.File, src []byte) string {
	var b strings.Builder
	cursor := 0 // byte offset into src
	for _, decl := range astFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		open := fset.Position(fn.Body.Lbrace).Offset
		closing := fset.Position(fn.Body.Rbrace).Offset
		bodyLines := fset.Position(fn.Body.Rbrace).Line - fset.Position(fn.Body.Lbrace).Line - 1
		if bodyLines < 0 {
			bodyLines = 0
		}
		b.Write(src[cursor:open])
		if bodyLines == 0 {
			b.WriteString("{ … }")
		} else {
			fmt.Fprintf(&b, "{ /* %d lines */ }", bodyLines)
		}
		cursor = closing + 1
	}
	b.Write(src[cursor:])
	return b.String()
}

// buildSkeletonOutline produces the structured --json outline.
func buildSkeletonOutline(file string, fset *token.FileSet, astFile *ast.File, src []byte) skeletonOutline {
	out := skeletonOutline{File: file, Package: astFile.Name.Name, Decls: []skeletonDecl{}}
	for _, decl := range astFile.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			out.Decls = append(out.Decls, funcSkeletonDecl(fset, d, src))
		case *ast.GenDecl:
			out.Decls = append(out.Decls, genSkeletonDecls(fset, d)...)
		}
	}
	return out
}

func funcSkeletonDecl(fset *token.FileSet, fn *ast.FuncDecl, src []byte) skeletonDecl {
	kind := "func"
	recv := ""
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		kind = "method"
		recv = receiverString(fset, fn, src)
	}
	sd := skeletonDecl{
		Kind:      kind,
		Name:      fn.Name.Name,
		Receiver:  recv,
		Signature: funcSignatureString(fset, fn, src),
		Doc:       docText(fn.Doc),
		StartLine: fset.Position(fn.Pos()).Line,
		EndLine:   fset.Position(fn.End()).Line,
	}
	if fn.Body != nil {
		sd.BodyLines = fset.Position(fn.Body.Rbrace).Line - fset.Position(fn.Body.Lbrace).Line - 1
		if sd.BodyLines < 0 {
			sd.BodyLines = 0
		}
	}
	return sd
}

func genSkeletonDecls(fset *token.FileSet, d *ast.GenDecl) []skeletonDecl {
	kind := strings.ToLower(d.Tok.String())
	var out []skeletonDecl
	for _, spec := range d.Specs {
		sd := skeletonDecl{
			Kind:      kind,
			StartLine: fset.Position(spec.Pos()).Line,
			EndLine:   fset.Position(spec.End()).Line,
			Doc:       docText(d.Doc),
		}
		switch s := spec.(type) {
		case *ast.TypeSpec:
			sd.Name = s.Name.Name
			if s.Doc != nil {
				sd.Doc = docText(s.Doc)
			}
		case *ast.ValueSpec:
			names := make([]string, 0, len(s.Names))
			for _, n := range s.Names {
				names = append(names, n.Name)
			}
			sd.Name = strings.Join(names, ", ")
		case *ast.ImportSpec:
			sd.Name = strings.Trim(s.Path.Value, `"`)
		}
		out = append(out, sd)
	}
	return out
}

// funcSignatureString returns the exact source text of a function from its
// `func` keyword to just before the body's opening brace.
func funcSignatureString(fset *token.FileSet, fn *ast.FuncDecl, src []byte) string {
	start := fset.Position(fn.Pos()).Offset
	end := fset.Position(fn.Type.End()).Offset
	return strings.TrimSpace(string(src[start:end]))
}

func receiverString(fset *token.FileSet, fn *ast.FuncDecl, src []byte) string {
	start := fset.Position(fn.Recv.List[0].Type.Pos()).Offset
	end := fset.Position(fn.Recv.List[0].Type.End()).Offset
	return string(src[start:end])
}

func docText(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	return strings.TrimSpace(cg.Text())
}
