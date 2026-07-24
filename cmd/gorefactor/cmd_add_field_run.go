package main

import (
	"fmt"
	goparser "go/parser"
	"go/token"
	"os"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

// addFieldCommand adds a field to a struct. Positional (unkeyed) composite
// literals of the struct elsewhere in the package would stop compiling, so
// the command detects them: with --update-literals they are rewritten to
// keyed form first; without it they are listed as warnings and the field is
// added anyway.
func addFieldCommand(args []string) error {
	pos, flags := parseFlags(args, addFieldFlags)
	if len(pos) < 3 {
		return usageErrorf("usage: add-field <file> <Struct> \"<Name> <Type> [`tag`]\" [--after FieldName] [--update-literals]")
	}
	file := pos[0]
	structName := pos[1]
	fieldSpec := strings.TrimSpace(pos[2])
	afterField := flags["--after"]
	updateLiterals := flags["--update-literals"] != ""

	m := &mutation{op: "add-field", file: file, files: packageGoFiles(file)}
	m.setCommonFlags(flags)

	if err := validateFieldSpec(fieldSpec); err != nil {
		return m.fail(err)
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

	st := findStructType(node, structName)
	if st == nil {
		_, all, derr := fileDecls(file)
		if derr != nil {
			all = nil
		}
		return m.fail(notFoundError(
			fmt.Sprintf("struct %q not found in %s", structName, file),
			structName, all))
	}

	fieldNames := structFieldNames(st)
	insertOffset := fset.Position(st.Fields.Closing).Offset
	insertText := "\t" + fieldSpec + "\n"
	if afterField != "" {
		f := findStructField(st, afterField)
		if f == nil {
			return m.fail(notFoundError(
				fmt.Sprintf("field %q not found in struct %s", afterField, structName),
				afterField, fieldNames))
		}
		insertOffset = fset.Position(f.End()).Offset
		insertText = "\n\t" + fieldSpec
	}

	// Detect positional literals across the package before mutating anything.
	positional, err := findPositionalLiterals(file, structName)
	if err != nil {
		return m.fail(err)
	}
	if err := warnOrValidatePositionalLiterals(positional, fieldNames, structName, updateLiterals); err != nil {
		return m.fail(err)
	}

	return m.run(func() (string, error) {
		return applyAddField(file, structName, fieldSpec, afterField, insertText, insertOffset, fieldNames, positional, updateLiterals)
	})
}
func warnOrValidatePositionalLiterals(positional []positionalLiteral, fieldNames []string, structName string, updateLiterals bool) error {
	if len(positional) > 0 && !updateLiterals {
		fmt.Fprintf(os.Stderr, "warning: %d positional literal(s) of %s will not compile after adding a field (use --update-literals):\n", len(positional), structName)
		for _, p := range positional {
			fmt.Fprintf(os.Stderr, "  %s:%d\n", p.file, p.line)
		}
	}
	if updateLiterals {
		for _, p := range positional {
			if len(p.elts) != len(fieldNames) {
				return fmt.Errorf(
					"cannot rewrite positional literal at %s:%d: %d value(s) but struct %s has %d field(s)",
					p.file, p.line, len(p.elts), structName, len(fieldNames))
			}
		}
	}
	return nil
}

func applyAddField(file, structName, fieldSpec, afterField, insertText string, insertOffset int, fieldNames []string, positional []positionalLiteral, updateLiterals bool) (string, error) {

	rewritten := 0
	if updateLiterals && len(positional) > 0 {
		n, err := rewritePositionalLiterals(positional, fieldNames)
		if err != nil {
			return "", err
		}
		rewritten = n
	}

	cur, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	off := insertOffset
	if updateLiterals && rewritten > 0 {

		fset := token.NewFileSet()
		node, perr := goparser.ParseFile(fset, file, cur, goparser.ParseComments)
		if perr != nil {
			return "", parseErrorf("re-parse %s after literal rewrite: %v", file, perr)
		}
		st := findStructType(node, structName)
		if st == nil {
			return "", fmt.Errorf("struct %s disappeared after literal rewrite", structName)
		}
		if afterField != "" {
			f := findStructField(st, afterField)
			if f == nil {
				return "", fmt.Errorf("field %s disappeared after literal rewrite", afterField)
			}
			off = fset.Position(f.End()).Offset
		} else {
			off = fset.Position(st.Fields.Closing).Offset
		}
	}

	var out []byte
	out = append(out, cur[:off]...)
	out = append(out, []byte(insertText)...)
	out = append(out, cur[off:]...)
	if _, perr := goparser.ParseFile(token.NewFileSet(), file, out, 0); perr != nil {
		return "", parseErrorf("adding field would produce a malformed file: %v", perr)
	}
	if err := os.WriteFile(file, out, 0644); err != nil {
		return "", err
	}
	if err := orchestrator.FormatImports(file); err != nil {
		fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
	}

	detail := fmt.Sprintf("Added field %q to struct %s in %s", fieldSpec, structName, file)
	if rewritten > 0 {
		detail += fmt.Sprintf(" (rewrote %d positional literal(s) to keyed form)", rewritten)
	} else if len(positional) > 0 {
		detail += fmt.Sprintf(" (%d positional literal(s) left unkeyed — see warnings)", len(positional))
	}
	return detail, nil
}
