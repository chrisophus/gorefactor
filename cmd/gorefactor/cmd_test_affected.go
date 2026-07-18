package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

var testAffectedFlags = map[string]bool{"--json": false, "--run": false, "--base": true}

func init() {
	registerCommand(Command{
		Name:        "test-affected",
		Description: "Map changed files (vs git base, default HEAD) to transitively affected packages and their tests [--run] [--json]",
		Usage:       "test-affected [--run] [--base ref] [--json]",
		MinArgs:     0,
		MaxArgs:     0,
		Flags:       testAffectedFlags,
		Run:         testAffectedCommand,
	})
}

// affectedPackage is one package whose tests are impacted by the change set.
type affectedPackage struct {
	Path     string `json:"path"`     // go test target, e.g. ./analyzer
	HasTests bool   `json:"hasTests"` // whether the package contains _test.go files
	Direct   bool   `json:"direct"`   // directly changed vs affected via reverse imports
}

type testAffectedResult struct {
	Base         string            `json:"base"`
	ChangedFiles []string          `json:"changedFiles"`
	Packages     []affectedPackage `json:"packages"`
	Ran          bool              `json:"ran"`
	Passed       bool              `json:"passed,omitempty"`
	TestOutput   string            `json:"testOutput,omitempty"`
}

func testAffectedCommand(args []string) error {
	_, flags := parseFlags(args, testAffectedFlags)
	base := "HEAD"
	if flags["--base"] != "" {
		base = flags["--base"]
	}

	res, err := computeTestAffected(base)
	if err != nil {
		return err
	}

	var runErr error
	if flags["--run"] != "" && len(res.Packages) > 0 {
		res.Ran = true
		targets := make([]string, 0, len(res.Packages))
		for _, p := range res.Packages {
			if p.HasTests {
				targets = append(targets, p.Path)
			}
		}
		if len(targets) > 0 {
			out, terr := exec.Command("go", append([]string{"test"}, targets...)...).CombinedOutput()
			res.Passed = terr == nil
			res.TestOutput = strings.TrimSpace(string(out))
			if terr != nil {
				runErr = gateErrorf("test-affected: go test failed for %s", strings.Join(targets, " "))
			}
		} else {
			res.Passed = true
		}
	}

	if flags["--json"] != "" {
		emitJSON(res)
		return runErr
	}
	fmt.Printf("changed vs %s: %d file(s) -> %d affected package(s)\n", res.Base, len(res.ChangedFiles), len(res.Packages))
	for _, p := range res.Packages {
		kind := "via reverse import"
		if p.Direct {
			kind = "directly changed"
		}
		tests := "tests"
		if !p.HasTests {
			tests = "no tests"
		}
		fmt.Printf("  %-30s %s, %s\n", p.Path, kind, tests)
	}
	if res.Ran {
		verdict := "PASS"
		if !res.Passed {
			verdict = "FAIL"
		}
		fmt.Printf("\ngo test: %s\n%s\n", verdict, res.TestOutput)
	}
	return runErr
}

// computeTestAffected maps changed .go files to packages and expands them
// through the reverse import graph built from find-package-deps machinery.
func computeTestAffected(base string) (*testAffectedResult, error) {
	prefix, err := gitShowPrefix()
	if err != nil {
		return nil, fmt.Errorf("test-affected requires a git repository: %w", err)
	}
	changed, err := gitChangedFiles(base, prefix)
	if err != nil {
		return nil, err
	}
	res := &testAffectedResult{Base: base, ChangedFiles: changed, Packages: []affectedPackage{}}
	if len(changed) == 0 {
		return res, nil
	}

	// Directly changed package dirs (graph keys: "" is the module root).
	direct := map[string]bool{}
	for _, f := range changed {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(f))
		if dir == "." {
			dir = ""
		}
		direct[dir] = true
	}
	if len(direct) == 0 {
		return res, nil
	}

	graph, err := analyzer.NewPackageGraph(".")
	if err != nil {
		return nil, fmt.Errorf("failed to build dependency graph: %w", err)
	}
	reverse := reverseImportGraph(graph, moduleImportPath("."))

	// BFS through reverse imports.
	affected := map[string]bool{}
	queue := make([]string, 0, len(direct))
	for d := range direct {
		affected[d] = true
		queue = append(queue, d)
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, importer := range reverse[cur] {
			if !affected[importer] {
				affected[importer] = true
				queue = append(queue, importer)
			}
		}
	}

	paths := make([]string, 0, len(affected))
	for p := range affected {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		target := "./" + p
		dir := p
		if p == "" {
			target = "."
			dir = "."
		}
		res.Packages = append(res.Packages, affectedPackage{
			Path:     target,
			HasTests: dirHasTests(dir),
			Direct:   direct[p],
		})
	}
	return res, nil
}


// gitChangedFiles returns files changed vs base (plus untracked files),
// relative to the current directory.
func gitChangedFiles(base, prefix string) ([]string, error) {
	out, err := exec.Command("git", "diff", "--name-only", base).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s failed: %w", base, err)
	}
	untracked, _ := exec.Command("git", "ls-files", "--others", "--exclude-standard").Output()

	seen := map[string]bool{}
	var files []string
	for _, line := range strings.Split(string(out)+"\n"+string(untracked), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, prefix) {
			continue
		}
		rel := strings.TrimPrefix(line, prefix)
		if !seen[rel] {
			seen[rel] = true
			files = append(files, rel)
		}
	}
	sort.Strings(files)
	return files, nil
}

// reverseImportGraph maps each in-module package path to the packages that
// import it. Keys/values use the graph's relative paths ("" = module root).
func reverseImportGraph(graph *analyzer.PackageGraph, module string) map[string][]string {
	reverse := map[string][]string{}
	if module == "" {
		return reverse
	}
	for _, pkg := range graph.AllPackages() {
		for _, imp := range pkg.Imports {
			var rel string
			switch {
			case imp == module:
				rel = ""
			case strings.HasPrefix(imp, module+"/"):
				rel = strings.TrimPrefix(imp, module+"/")
			default:
				continue // out-of-module import
			}
			reverse[rel] = append(reverse[rel], pkg.Path)
		}
	}
	return reverse
}

// moduleImportPath reads the module path from go.mod in dir, or "".
func moduleImportPath(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

func dirHasTests(dir string) bool {
	matches, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	return err == nil && len(matches) > 0
}
