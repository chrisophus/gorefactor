package main

import (
	"fmt"
	goparser "go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/orchestrator"
)

var (
	createFlags  = mutFlagSpec(nil)
	insertFlags  = mutFlagSpec(nil)
	replaceFlags = mutFlagSpec(nil)
	deleteFlags  = mutFlagSpec(map[string]bool{"--safe": false})
	renameFlags  = mutFlagSpec(map[string]bool{"--strict": false})
)

func init() {
	registerCommand(Command{
		Name:        "create",
		Description: "Create a new file with content from arg or stdin",
		Usage:       "create <path> [content|-] [--json] [--dry-run] [--gate]",
		MinArgs:     1,
		MaxArgs:     2,
		Flags:       createFlags,
		Run:         createCommand,
	})
	registerCommand(Command{
		Name:        "insert",
		Description: "Insert code into a file at a location (at-end | at-beginning | before:Func | after:Func | inside:Func)",
		Usage:       "insert <file> <at-end|at-beginning|before:Func|after:Func|inside:Func> [content|-] [--json] [--dry-run] [--gate]",
		MinArgs:     2,
		MaxArgs:     3,
		Flags:       insertFlags,
		Run:         insertCommand,
	})
	registerCommand(Command{
		Name:        "replace",
		Description: "Replace a code pattern inside a function/method (AST: pattern must be a full statement)",
		Usage:       "replace <file> <Func|Receiver:Method> <old-stmt> <new-stmt> [--json] [--dry-run] [--gate]",
		MinArgs:     4,
		MaxArgs:     4,
		Flags:       replaceFlags,
		Run:         replaceCommand,
	})
	registerCommand(Command{
		Name:        "delete",
		Description: "Delete a declaration (function, method, or type) from a file",
		Usage:       "delete <file> <Func|Receiver:Method> [--safe] [--json] [--dry-run] [--gate]",
		MinArgs:     2,
		MaxArgs:     2,
		Flags:       deleteFlags,
		Run:         deleteCommand,
	})
	registerCommand(Command{
		Name:        "rename",
		Description: "Rename an unexported symbol across the package",
		Usage:       "rename <file> <oldname> <newname> [--strict] [--json] [--dry-run] [--gate]",
		MinArgs:     3,
		MaxArgs:     3,
		Flags:       renameFlags,
		Run:         renameCommand,
	})
}

func readContentArg(args []string, idx int) (string, error) {
	if idx < len(args) && args[idx] != "-" {
		return args[idx], nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// validateGoSnippet checks that content parses as a complete Go file, as
// top-level declarations, or as statements. Returns an exit-3 error when
// none of the forms parse.
func validateGoSnippet(content string) error {
	fset := token.NewFileSet()
	_, fileErr := goparser.ParseFile(fset, "snippet.go", content, 0)
	if fileErr == nil {
		return nil
	}
	if _, err := goparser.ParseFile(fset, "snippet.go", "package p\n"+content, 0); err == nil {
		return nil
	}
	if _, err := goparser.ParseFile(fset, "snippet.go", "package p\nfunc _() {\n"+content+"\n}", 0); err == nil {
		return nil
	}
	return parseErrorf("content does not parse as a Go file, declarations, or statements: %v", fileErr)
}

func createCommand(args []string) error {
	pos, flags := parseFlags(args, createFlags)
	if len(pos) < 1 {
		return usageErrorf("usage: create <path> [content] (else stdin)")
	}
	path := pos[0]
	content, err := readContentArg(pos, 1)
	if err != nil {
		return err
	}
	m := &mutation{op: "create", file: path}
	m.setCommonFlags(flags)

	if _, err := os.Stat(path); err == nil {
		return m.fail(fmt.Errorf("file already exists: %s (use replace or insert)", path))
	}
	if strings.HasSuffix(path, ".go") {
		fset := token.NewFileSet()
		if _, perr := goparser.ParseFile(fset, path, content, 0); perr != nil {
			return m.fail(parseErrorf("content does not parse as a Go file: %v", perr))
		}
	}
	return m.run(func() (string, error) {
		if dir := filepath.Dir(path); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return "", err
			}
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return "", err
		}
		if strings.HasSuffix(path, ".go") {
			if err := orchestrator.FormatImports(path); err != nil {
				fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", path, err)
			}
		}
		return fmt.Sprintf("Created %s", path), nil
	})
}

func parseLocSpec(s string) (*orchestrator.InsertionLocation, error) {
	switch {
	case s == "at-end":
		return &orchestrator.InsertionLocation{Type: "at_end"}, nil
	case s == "at-beginning":
		return &orchestrator.InsertionLocation{Type: "at_beginning"}, nil
	case strings.HasPrefix(s, "before:"):
		return &orchestrator.InsertionLocation{Type: "before_function", FunctionName: strings.TrimPrefix(s, "before:")}, nil
	case strings.HasPrefix(s, "after:"):
		return &orchestrator.InsertionLocation{Type: "after_function", FunctionName: strings.TrimPrefix(s, "after:")}, nil
	case strings.HasPrefix(s, "inside:"):
		return &orchestrator.InsertionLocation{Type: "inside_function", FunctionName: strings.TrimPrefix(s, "inside:")}, nil
	}
	return nil, usageErrorf("unknown location %q; valid forms: at-end | at-beginning | before:Func | after:Func | inside:Func", s)
}

func insertCommand(args []string) error {
	pos, flags := parseFlags(args, insertFlags)
	if len(pos) < 2 {
		return usageErrorf("usage: insert <file> <at-end|at-beginning|before:Func|after:Func|inside:Func> [content] (else stdin)")
	}
	file := pos[0]
	m := &mutation{op: "insert", file: file}
	m.setCommonFlags(flags)
	loc, err := parseLocSpec(pos[1])
	if err != nil {
		return m.fail(err)
	}
	content, err := readContentArg(pos, 2)
	if err != nil {
		return m.fail(err)
	}
	if err := validateFuncTarget(file, loc); err != nil {
		return m.fail(err)
	}
	if err := validateGoSnippet(content); err != nil {
		return m.fail(err)
	}
	return m.run(func() (string, error) {
		ci := orchestrator.NewCodeInserter()
		ins, err := ci.InsertCode(file, loc, content)
		if err != nil {
			return "", err
		}
		if err := orchestrator.FormatImports(file); err != nil {
			fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
		}
		return fmt.Sprintf("Inserted into %s at lines %d-%d", file, ins.StartLine, ins.EndLine), nil
	})
}

func parseFuncLocator(s string) (*orchestrator.InsertionLocation, error) {
	if i := strings.Index(s, ":"); i >= 0 {
		return &orchestrator.InsertionLocation{
			ReceiverType: s[:i],
			MethodName:   s[i+1:],
		}, nil
	}
	return &orchestrator.InsertionLocation{FunctionName: s}, nil
}

func deleteCommand(args []string) error {
	pos, flags := parseFlags(args, deleteFlags)
	if len(pos) < 2 {
		return usageErrorf("usage: delete <file> <funcname-or-Receiver:Method> [--safe]")
	}
	file := pos[0]
	name := pos[1]
	safe := flags["--safe"] != ""
	m := &mutation{op: "delete", file: file}
	m.setCommonFlags(flags)

	if err := validateDeclTarget(file, name); err != nil {
		return m.fail(err)
	}

	if safe {
		pkgDir := filepath.Dir(file)
		target := parseTargetSpec(name)
		funcName := target.FunctionName
		if funcName == "" {
			funcName = target.MethodName
		}
		if funcName != "" {
			pkgFiles, _ := collectGoFiles(pkgDir, analyzer.DefaultWalkOptions())
			ca := analyzer.NewCallAnalyzer(pkgFiles)
			receiverType := ""
			if target.ReceiverType != "" {
				receiverType = target.ReceiverType
			}
			analysis, err := ca.FindCallers(funcName, receiverType)
			if err == nil && len(analysis.DirectCallers) > 0 {
				fmt.Fprintf(os.Stderr, "error: %s has %d caller(s) — delete would break the build:\n", name, len(analysis.DirectCallers))
				for _, c := range analysis.DirectCallers {
					fmt.Fprintf(os.Stderr, "  %s:%d  %s\n", c.File, c.Line, c.CallerName)
				}
				return m.fail(fmt.Errorf("use find-callers %s to review, then remove callers before deleting", name))
			}
		}
	}

	return m.run(func() (string, error) {
		target := parseTargetSpec(name)
		err := runPlanOps("delete-decl", []*orchestrator.RefactoringOperation{{
			Type:   "delete_declaration",
			File:   file,
			Target: target,
		}})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Deleted %s from %s", name, file), nil
	})
}

func parseTargetSpec(s string) *orchestrator.TargetSpecification {
	if i := strings.Index(s, ":"); i >= 0 {
		return &orchestrator.TargetSpecification{
			ReceiverType: s[:i],
			MethodName:   s[i+1:],
		}
	}
	return &orchestrator.TargetSpecification{FunctionName: s}
}
func replaceCommand(args []string) error {
	pos, flags := parseFlags(args, replaceFlags)
	if len(pos) < 4 {
		return usageErrorf("usage: replace <file> <funcname-or-Receiver:Method> <pattern> <replacement>")
	}
	file := pos[0]
	m := &mutation{op: "replace", file: file}
	m.setCommonFlags(flags)
	loc, err := parseFuncLocator(pos[1])
	if err != nil {
		return m.fail(err)
	}
	pattern := pos[2]
	replacement := pos[3]
	if err := validateFuncTarget(file, loc); err != nil {
		return m.fail(err)
	}
	if err := validateGoSnippet(replacement); err != nil {
		return m.fail(err)
	}
	return m.run(func() (string, error) {
		ci := orchestrator.NewCodeInserter()
		ins, err := ci.ReplaceCodeBlock(file, loc, pattern, replacement)
		if err != nil {
			if strings.Contains(err.Error(), "no statement matching") {
				return "", notFoundErrorf("%v\nhint: the pattern must be a complete statement; use replace-text for partial text", err)
			}
			return "", err
		}
		if err := orchestrator.FormatImports(file); err != nil {
			fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
		}
		return fmt.Sprintf("Replaced in %s at lines %d-%d", file, ins.StartLine, ins.EndLine), nil
	})
}
