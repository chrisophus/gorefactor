package doctor

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

// Temporal is the workflow-determinism substrate (design plan step 5).
// Temporal replays workflow code against recorded history, so workflow
// functions must be deterministic: code that compiles, tests green, and works
// in an ordinary program silently corrupts workflows — exactly the
// agent-written failure mode doctor exists to catch.
//
// Reuse-first per the plan: when Temporal's official workflowcheck analyzer
// is on PATH it runs as the engine (type-checked, call-graph aware);
// otherwise an in-process AST scan of workflow.Context functions catches the
// plan's named violations (time.Now/Sleep, math/rand, naked goroutines,
// native select/channels). Wrapping workflowcheck as a library was evaluated
// and rejected: it would pull go.temporal.io/sdk into every gorefactor build
// for a shape most modules don't have.
type Temporal struct{}

// workflowImportPath is the Temporal workflow package workflow code imports.
const workflowImportPath = "go.temporal.io/sdk/workflow"

// Info implements Substrate. Gating: tmprl findings are error severity —
// nondeterminism corrupts running workflows on replay.
func (Temporal) Info() SubstrateInfo {
	return SubstrateInfo{Name: "temporal", Gating: true, ScopeCapable: true}
}

// Run implements Substrate. Modules that don't require go.temporal.io/sdk
// trivially pass: the substrate ran and the shape has nothing to check.
func (t Temporal) Run(ctx RunContext) ([]Finding, error) {
	shape, err := DetectShape(ctx.Root)
	if err != nil {
		return nil, unavailablef("shape detection failed: %v", err)
	}
	if !shape.HasTemporal {
		return nil, nil
	}
	if findings, ok, werr := runWorkflowcheck(ctx); ok {
		return findings, werr
	}
	return t.scanInProcess(ctx)
}

// scanInProcess is the fallback engine: a parse-only scan of every function
// taking a workflow.Context parameter.
func (Temporal) scanInProcess(ctx RunContext) ([]Finding, error) {
	files, err := temporalScopeFiles(ctx)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		astFile, perr := parser.ParseFile(fset, f, nil, 0)
		if perr != nil {
			continue
		}
		imports := temporalImports(astFile)
		if imports.workflow == "" {
			continue
		}
		rel, rerr := filepath.Rel(ctx.Root, f)
		if rerr != nil {
			rel = f
		}
		findings = append(findings, scanWorkflowFile(astFile, fset, filepath.ToSlash(rel), imports)...)
	}
	return findings, nil
}

// temporalScopeFiles honors ScopeDirs (the substrate is scope-capable).
func temporalScopeFiles(ctx RunContext) ([]string, error) {
	if len(ctx.ScopeDirs) == 0 {
		files, err := analyzer.WalkGoFiles(ctx.Root, analyzer.DefaultWalkOptions())
		if err != nil {
			return nil, fmt.Errorf("walk: %w", err)
		}
		return files, nil
	}
	var files []string
	for _, dir := range ctx.ScopeDirs {
		matches, err := filepath.Glob(filepath.Join(ctx.Root, dir, "*.go"))
		if err != nil {
			return nil, fmt.Errorf("glob: %w", err)
		}
		files = append(files, matches...)
	}
	return files, nil
}

// temporalFileImports carries the file-local names of the packages the scan
// needs to recognize.
type temporalFileImports struct {
	workflow string // local name of go.temporal.io/sdk/workflow
	timePkg  string // local name of time
	randPkg  string // local name of math/rand (or math/rand/v2)
}

func temporalImports(f *ast.File) temporalFileImports {
	out := temporalFileImports{}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name
		}
		switch path {
		case workflowImportPath:
			out.workflow = importLocalName(name, "workflow")
		case "time":
			out.timePkg = importLocalName(name, "time")
		case "math/rand", "math/rand/v2":
			out.randPkg = importLocalName(name, "rand")
		}
	}
	return out
}

func importLocalName(explicit, def string) string {
	if explicit != "" {
		return explicit
	}
	return def
}

// scanWorkflowFile emits findings for determinism violations inside functions
// that take a workflow.Context parameter (including violations in nested
// function literals — replay executes those too).
func scanWorkflowFile(f *ast.File, fset *token.FileSet, rel string, imports temporalFileImports) []Finding {
	var findings []Finding
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil || !hasWorkflowContextParam(fd, imports.workflow) {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if rule, msg := checkWorkflowNode(n, imports); rule != "" {
				findings = append(findings, Finding{
					File:     rel,
					Line:     fset.Position(n.Pos()).Line,
					Rule:     rule,
					Category: CategoryTemporal,
					Message:  fmt.Sprintf("workflow %s: %s", fd.Name.Name, msg),
				})
			}
			return true
		})
	}
	return findings
}

// hasWorkflowContextParam reports whether fd takes a workflow.Context.
func hasWorkflowContextParam(fd *ast.FuncDecl, workflowPkg string) bool {
	for _, p := range fd.Type.Params.List {
		sel, ok := p.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Context" {
			continue
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == workflowPkg {
			return true
		}
	}
	return false
}

// nonDeterministicTimeCalls maps time-package calls to their workflow-safe
// replacement.
var nonDeterministicTimeCalls = map[string]string{
	"Now":       "workflow.Now",
	"Sleep":     "workflow.Sleep",
	"After":     "workflow.NewTimer",
	"Tick":      "workflow.NewTimer",
	"NewTimer":  "workflow.NewTimer",
	"NewTicker": "workflow.NewTimer",
}

// checkWorkflowNode classifies one AST node inside a workflow function,
// returning a rule name and message when it is a determinism violation.
func checkWorkflowNode(n ast.Node, imports temporalFileImports) (string, string) {
	switch node := n.(type) {
	case *ast.GoStmt:
		return "temporal/goroutine", "naked goroutine — replay cannot order it; use workflow.Go"
	case *ast.SelectStmt:
		return "temporal/select", "native select — nondeterministic under replay; use workflow.Selector"
	case *ast.CallExpr:
		return checkWorkflowCall(node, imports)
	}
	return "", ""
}

func checkWorkflowCall(call *ast.CallExpr, imports temporalFileImports) (string, string) {
	if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "make" && len(call.Args) > 0 {
		if _, isChan := call.Args[0].(*ast.ChanType); isChan {
			return "temporal/channel", "native channel — nondeterministic under replay; use workflow.NewChannel"
		}
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", ""
	}
	if imports.timePkg != "" && pkg.Name == imports.timePkg {
		if repl, bad := nonDeterministicTimeCalls[sel.Sel.Name]; bad {
			return "temporal/time", fmt.Sprintf("time.%s — wall-clock differs on replay; use %s", sel.Sel.Name, repl)
		}
	}
	if imports.randPkg != "" && pkg.Name == imports.randPkg {
		return "temporal/rand", fmt.Sprintf("rand.%s — random values differ on replay; use workflow.SideEffect", sel.Sel.Name)
	}
	return "", ""
}
