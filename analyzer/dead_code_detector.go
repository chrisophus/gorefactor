package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// DeadCodeDetector finds unused functions and fields
type DeadCodeDetector struct {
	files []string
}

// NewDeadCodeDetector creates a detector for a set of files
func NewDeadCodeDetector(files []string) *DeadCodeDetector {
	return &DeadCodeDetector{
		files: files,
	}
}

// DeadCodeIssue represents a dead code problem
type DeadCodeIssue struct {
	Type        string // "function", "method", "field"
	Name        string
	Receiver    string // for methods
	File        string
	Line        int
	CallerCount int
	IsExported  bool
	Reason      string
}

// DetectDeadFunctions finds functions that are never called
func (dcd *DeadCodeDetector) DetectDeadFunctions() []DeadCodeIssue {
	var issues []DeadCodeIssue

	// Collect all function definitions
	funcMap := make(map[string]FuncDef) // key: name or receiver:name
	for _, f := range dcd.files {
		funcs := extractFunctions(f)
		for _, fn := range funcs {
			key := fn.Key()
			funcMap[key] = fn
		}
	}

	// Count every identifier occurrence across the package once up front.
	identFreq := dcd.identFrequency()

	// Check each function for references
	for _, fn := range funcMap {
		// Skip main(), init(), and test functions
		if fn.IsMain() || fn.IsInit() || fn.IsTest() {
			continue
		}

		// Skip exported functions (can be called from outside)
		if fn.IsExported {
			continue
		}

		// A symbol is dead only if its name appears nowhere in the package
		// outside its own declaration. Counting every identifier occurrence
		// (not just call expressions) is what catches functions referenced as
		// values — stored in a command table, passed as an argument, or taken
		// as a method value — which FindCallers and FindAllUses both miss
		// because they only see call sites. (That blind spot is exactly what
		// let the dead-code autofix delete live command handlers.) For a
		// destructive autofix, keeping anything possibly-referenced is the
		// safe default; we would rather under-report than delete live code.
		if identFreq[fn.Name] > 1 {
			continue
		}

		// No reference anywhere but the declaration itself — dead code.
		issue := DeadCodeIssue{
			Type:        "function",
			Name:        fn.Name,
			Receiver:    fn.Receiver,
			File:        fn.File,
			Line:        fn.Line,
			CallerCount: 0,
			IsExported:  fn.IsExported,
			Reason:      "Never called",
		}
		if fn.Receiver != "" {
			issue.Type = "method"
		}
		issues = append(issues, issue)
	}

	return issues
}

// FuncDef represents a function or method definition
type FuncDef struct {
	Name       string
	Receiver   string
	File       string
	Line       int
	IsExported bool
}

// Key returns a unique key for this function
func (f FuncDef) Key() string {
	if f.Receiver != "" {
		return f.Receiver + ":" + f.Name
	}
	return f.Name
}

// IsMain returns true if this is the main function
func (f FuncDef) IsMain() bool {
	return f.Name == "main"
}

// IsInit returns true if this is an init function
func (f FuncDef) IsInit() bool {
	return f.Name == "init"
}

// IsTest returns true if this is a test function
func (f FuncDef) IsTest() bool {
	return strings.HasPrefix(f.Name, "Test") || strings.HasPrefix(f.Name, "Benchmark") || strings.HasPrefix(f.Name, "Example")
}

// extractFunctions extracts all function definitions from a file
func extractFunctions(filePath string) []FuncDef {
	var funcs []FuncDef

	content, err := readFileContent(filePath)
	if err != nil {
		return funcs
	}

	fset := token.NewFileSet()
	f, err := parseGoFile(fset, filePath, content)
	if err != nil {
		return funcs
	}

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		funcs = append(funcs, FuncDef{
			Name:       fn.Name.Name,
			Receiver:   receiverIdentName(fn),
			File:       filePath,
			Line:       fset.Position(fn.Pos()).Line,
			IsExported: fn.Name.IsExported(),
		})
	}

	return funcs

}

func receiverIdentName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	switch t := fn.Recv.List[0].Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			return ident.Name
		}
	}
	return ""
}

// readFileContent reads a file's contents
func readFileContent(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// parseGoFile parses a Go file
func parseGoFile(fset *token.FileSet, filePath, content string) (*ast.File, error) {
	return parser.ParseFile(fset, filePath, content, 0)
}

// Summary returns a string summary
func (d DeadCodeIssue) Summary() string {
	if d.Receiver != "" {
		return fmt.Sprintf("[%s] Dead Code: %s%s (%s) at %s:%d", d.Type, d.Receiver, d.Name, d.Reason, filepath.Base(d.File), d.Line)
	}
	return fmt.Sprintf("[%s] Dead Code: %s (%s) at %s:%d", d.Type, d.Name, d.Reason, filepath.Base(d.File), d.Line)
}

func (dcd *DeadCodeDetector) identFrequency() map[string]int {
	freq := make(map[string]int)
	for _, file := range dcd.files {
		content, err := readFileContent(file)
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		astFile, err := parseGoFile(fset, file, content)
		if err != nil {
			continue
		}
		ast.Inspect(astFile, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				freq[id.Name]++
			}
			return true
		})
	}
	return freq
}
