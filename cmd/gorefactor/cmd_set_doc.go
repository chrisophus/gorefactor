package main

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"strings"
)

var setDocFlags = mutFlagSpec(nil)

// docCommentWidth is the soft wrap column for generated doc comments,
// including the "// " prefix.
const docCommentWidth = 100

func init() {
	registerCommand(Command{
		Name:        "set-doc",
		Description: "Set or replace the doc comment on a top-level declaration (func, method, type, const/var)",
		Usage:       "set-doc <file> <Decl|Receiver:Method> [text|-] [--json] [--dry-run] [--gate]",
		MinArgs:     2,
		MaxArgs:     3,
		Flags:       setDocFlags,
		Run:         setDocCommand,
	})
}

// setDocCommand sets the doc comment of any top-level declaration. The text
// is taken as plain prose; it is wrapped at ~100 columns and prefixed with
// the declaration name per Go doc convention when missing.
func setDocCommand(args []string) error {
	pos, flags := parseFlags(args, setDocFlags)
	if len(pos) < 2 {
		return usageErrorf("usage: set-doc <file> <Decl|Receiver:Method> [text] (else stdin)")
	}
	file := pos[0]
	locator := pos[1]
	m := &mutation{op: "set-doc", file: file}
	m.setCommonFlags(flags)

	text, err := readContentArg(pos, 2)
	if err != nil {
		return m.fail(err)
	}
	if strings.TrimSpace(text) == "" {
		return m.fail(usageErrorf("set-doc requires non-empty text"))
	}

	src, err := os.ReadFile(file)
	if err != nil {
		return m.fail(err)
	}
	fset := token.NewFileSet()
	node, err := goparser.ParseFile(fset, file, src, goparser.ParseComments)
	if err != nil {
		return m.fail(parseErrorf("failed to parse %s: %v", file, err))
	}

	decl, docName, err := findTopLevelDecl(node, locator)
	if err != nil {
		_, all, derr := fileDecls(file)
		if derr != nil {
			all = nil
		}
		return m.fail(notFoundError(
			fmt.Sprintf("declaration %q not found in %s", locator, file),
			locator, all))
	}

	declStart := fset.Position(decl.Pos()).Offset
	replaceFrom := declStart
	if doc := declDoc(decl); doc != nil {
		replaceFrom = fset.Position(doc.Pos()).Offset
	}
	comment := formatDocComment(docName, text)

	var out []byte
	out = append(out, src[:replaceFrom]...)
	out = append(out, []byte(comment)...)
	out = append(out, src[declStart:]...)

	return m.validateAndWrite(file, out, "the doc comment",
		fmt.Sprintf("Set doc comment on %s in %s", locator, file))
}

// findTopLevelDecl locates a top-level declaration by name. Functions are
// matched as "Name", methods as "Receiver:Method", and GenDecls (type, var,
// const) when any contained spec declares the name. docName is the name the
// doc comment should start with (the method name for Receiver:Method).
func findTopLevelDecl(node *ast.File, locator string) (decl ast.Decl, docName string, err error) {
	recv, name := "", locator
	if i := strings.Index(locator, ":"); i >= 0 {
		recv, name = locator[:i], locator[i+1:]
	}
	for _, d := range node.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Name.Name != name {
				continue
			}
			declRecv := ""
			if d.Recv != nil && len(d.Recv.List) > 0 {
				declRecv = receiverTypeName(d.Recv.List[0].Type)
			}
			if declRecv == recv {
				return d, name, nil
			}
		case *ast.GenDecl:
			if recv != "" {
				continue
			}
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.Name == name {
						return d, name, nil
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.Name == name {
							return d, name, nil
						}
					}
				}
			}
		}
	}
	return nil, "", fmt.Errorf("declaration %q not found", locator)
}

// declDoc returns the doc comment group attached to a top-level declaration.
func declDoc(decl ast.Decl) *ast.CommentGroup {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return d.Doc
	case *ast.GenDecl:
		return d.Doc
	}
	return nil
}

// formatDocComment renders text as a `// ` comment block wrapped at docCommentWidth columns,
// preserving paragraph breaks. Per Go convention the comment starts with the declaration name; it
// is prepended when missing.
func formatDocComment(name, text string) string {
	var b strings.Builder
	for pi, para := range docParagraphs(text) {
		if pi > 0 {
			b.WriteString("//\n")
		}
		words := strings.Fields(para)
		if pi == 0 && (len(words) == 0 || words[0] != name) {
			words = append([]string{name}, words...)
		}
		// Wrap by tracking the pending line's words and width instead of
		// concatenating into a string in the loop.
		var cur []string
		curLen := len("//")
		writeLine := func() {
			b.WriteString("//")
			for _, w := range cur {
				b.WriteString(" ")
				b.WriteString(w)
			}
			b.WriteString("\n")
		}
		for _, w := range words {
			if curLen+1+len(w) > docCommentWidth && len(cur) > 0 {
				writeLine()
				cur = cur[:0]
				curLen = len("//")
			}
			cur = append(cur, w)
			curLen += 1 + len(w)
		}
		writeLine()
	}
	return b.String()

}

func docParagraphs(text string) []string {
	var paras []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			paras = append(paras, strings.Join(cur, " "))
			cur = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimPrefix(line, "//"))
		if line == "" {
			flush()
			continue
		}
		cur = append(cur, line)
	}
	flush()
	return paras
}
