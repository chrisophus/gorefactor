package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

// oracle.go: structural intent-oracle for the agent corpus (Slice 3a-B).
//
// A green build+test proves the agent didn't BREAK the code; it does not prove
// the agent did the INTENDED transform (SWE-Refactor's finding). Each oracleCheck
// is a declarative predicate evaluated against the post-run fixture directory via
// the analyzer package, so a task passes only when the transform provably happened.

type oracleKind string

const (
	oracleDeclaredIn   oracleKind = "declared_in"
	oracleAbsentFrom   oracleKind = "absent_from"
	oracleUsesResolve  oracleKind = "uses_resolve"
	oracleAPIUnchanged oracleKind = "api_unchanged"
	oracleAPIAdded     oracleKind = "api_added"
	oracleASTMatches   oracleKind = "ast_matches"
	oracleASTAbsent    oracleKind = "ast_absent"
	oracleFilesTouched oracleKind = "files_touched"
)

// oracleCheck is one declarative assertion about the post-run fixture.
type oracleCheck struct {
	Kind    oracleKind
	Symbol  string   // Func or Receiver:Method (declared_in/absent_from/uses_resolve/api_added)
	File    string   // target file, relative to the fixture dir (declared_in/absent_from)
	Pattern string   // search-ast pattern (ast_matches/ast_absent)
	Files   []string // expected caller files (uses_resolve) or allowed changed set (files_touched)
}

// --- readable constructors for task definitions -------------------------------

func declaredIn(symbol, file string) oracleCheck {
	return oracleCheck{Kind: oracleDeclaredIn, Symbol: symbol, File: file}
}
func absentFrom(symbol, file string) oracleCheck {
	return oracleCheck{Kind: oracleAbsentFrom, Symbol: symbol, File: file}
}
func usesResolve(symbol string, files ...string) oracleCheck {
	return oracleCheck{Kind: oracleUsesResolve, Symbol: symbol, Files: files}
}
func apiUnchanged() oracleCheck { return oracleCheck{Kind: oracleAPIUnchanged} }
func apiAdded(symbol string) oracleCheck {
	return oracleCheck{Kind: oracleAPIAdded, Symbol: symbol}
}
func astMatches(pattern string) oracleCheck {
	return oracleCheck{Kind: oracleASTMatches, Pattern: pattern}
}
func astAbsent(pattern string) oracleCheck {
	return oracleCheck{Kind: oracleASTAbsent, Pattern: pattern}
}
func filesTouched(files ...string) oracleCheck {
	return oracleCheck{Kind: oracleFilesTouched, Files: files}
}

// evalOracle runs every check against dir. Returns overall pass plus one failure
// message per failed check. An empty check list always passes.
func evalOracle(dir string, checks []oracleCheck) (bool, []string) {
	var failures []string
	for _, c := range checks {
		ok, msg := evalOne(dir, c)
		if !ok {
			failures = append(failures, msg)
		}
	}
	return len(failures) == 0, failures
}

func evalOne(dir string, c oracleCheck) (bool, string) {
	switch c.Kind {
	case oracleDeclaredIn:
		if declOf(filepath.Join(dir, c.File), c.Symbol) {
			return true, ""
		}
		return false, fmt.Sprintf("declared_in: %s not declared in %s", c.Symbol, c.File)
	case oracleAbsentFrom:
		if !declOf(filepath.Join(dir, c.File), c.Symbol) {
			return true, ""
		}
		return false, fmt.Sprintf("absent_from: %s still declared in %s", c.Symbol, c.File)
	case oracleUsesResolve:
		return evalUsesResolve(dir, c)
	case oracleAPIUnchanged:
		return evalAPIUnchanged(dir)
	case oracleAPIAdded:
		return evalAPIAdded(dir, c.Symbol)
	case oracleASTMatches:
		ms, err := analyzer.SearchASTInDir(dir, c.Pattern)
		if err == nil && len(ms) > 0 {
			return true, ""
		}
		return false, fmt.Sprintf("ast_matches: no match for %q", c.Pattern)
	case oracleASTAbsent:
		ms, err := analyzer.SearchASTInDir(dir, c.Pattern)
		if err == nil && len(ms) == 0 {
			return true, ""
		}
		return false, fmt.Sprintf("ast_absent: %d match(es) for %q", len(ms), c.Pattern)
	case oracleFilesTouched:
		return evalFilesTouched(dir, c.Files)
	}
	return false, fmt.Sprintf("unknown oracle kind %q", c.Kind)
}

// declOf reports whether symbol is declared at top level in file. symbol is a
// plain name (func/type/const/var) or "Receiver:Method".
func declOf(file, symbol string) bool {
	recv, name := "", symbol
	if i := strings.IndexByte(symbol, ':'); i >= 0 {
		recv, name = symbol[:i], symbol[i+1:]
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		return false
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name.Name != name {
				continue
			}
			if recv == "" {
				if d.Recv == nil {
					return true
				}
				continue
			}
			if analyzer.FuncReceiverName(d) == recv {
				return true
			}
		case *ast.GenDecl:
			if recv != "" {
				continue
			}
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.Name == name {
						return true
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.Name == name {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

func evalUsesResolve(dir string, c oracleCheck) (bool, string) {
	files, err := analyzer.WalkGoFiles(dir, analyzer.DefaultWalkOptions())
	if err != nil {
		return false, "uses_resolve: " + err.Error()
	}
	recv, name := "", c.Symbol
	if i := strings.IndexByte(c.Symbol, ':'); i >= 0 {
		recv, name = c.Symbol[:i], c.Symbol[i+1:]
	}
	uses, err := analyzer.NewUseAnalyzer(files).FindAllUses(analyzer.SymbolQuery{Name: name, Receiver: recv})
	if err != nil {
		return false, "uses_resolve: " + err.Error()
	}
	seen := map[string]bool{}
	for _, u := range uses {
		rel, rerr := filepath.Rel(dir, u.File)
		if rerr != nil {
			rel = u.File
		}
		seen[filepath.ToSlash(rel)] = true
	}
	var missing []string
	for _, want := range c.Files {
		if !seen[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) == 0 {
		return true, ""
	}
	return false, fmt.Sprintf("uses_resolve: %s not used in %s", c.Symbol, strings.Join(missing, ", "))
}

func evalAPIUnchanged(dir string) (bool, string) {
	res, err := analyzer.ComputeAPIDiff(dir, "HEAD")
	if err != nil {
		return false, "api_unchanged: " + err.Error()
	}
	if len(res.Added) == 0 && len(res.Removed) == 0 && len(res.Changed) == 0 {
		return true, ""
	}
	return false, fmt.Sprintf("api_unchanged: %d added, %d removed, %d changed",
		len(res.Added), len(res.Removed), len(res.Changed))
}

func evalAPIAdded(dir, symbol string) (bool, string) {
	res, err := analyzer.ComputeAPIDiff(dir, "HEAD")
	if err != nil {
		return false, "api_added: " + err.Error()
	}
	for _, a := range res.Added {
		// entries are "qualifier.Symbol signature"; match the symbol token.
		if head := strings.SplitN(a, " ", 2)[0]; head == symbol || strings.HasSuffix(head, "."+symbol) {
			return true, ""
		}
	}
	return false, fmt.Sprintf("api_added: %s not in added API surface", symbol)
}

func evalFilesTouched(dir string, allowed []string) (bool, string) {
	cmd := exec.Command("git", "diff", "--name-only", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false, "files_touched: " + err.Error()
	}
	allow := map[string]bool{}
	for _, f := range allowed {
		allow[f] = true
	}
	var extra []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		if !allow[line] {
			extra = append(extra, line)
		}
	}
	if len(extra) == 0 {
		return true, ""
	}
	return false, fmt.Sprintf("files_touched: unexpected changes to %s", strings.Join(extra, ", "))
}
