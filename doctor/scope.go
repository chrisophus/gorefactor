package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

// ChangedScope computes the package dirs (relative to root) touched vs baseRef
// — committed or not, plus untracked files — widened by their direct reverse
// dependencies (depth-1, plan open item 3). This is the scoped-tier input for
// scope-capable substrates.
func ChangedScope(root, baseRef string) ([]string, error) {
	changed, err := changedGoDirs(root, baseRef)
	if err != nil {
		return nil, fmt.Errorf("changed go dirs: %w", err)
	}
	if len(changed) == 0 {
		return nil, nil
	}
	scope := reverseDeps(root, changed)
	sort.Strings(scope)
	return scope, nil
}

func changedGoDirs(root, baseRef string) (map[string]bool, error) {
	diff, err := gitCmd(root, "diff", "--name-only", "--relative", baseRef, "--", "*.go").Output()

	if err != nil {
		return nil, fmt.Errorf("git diff vs %s: %w", baseRef, err)
	}
	untracked, err := gitCmd(root, "ls-files", "--others", "--exclude-standard", "--", "*.go").Output()

	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	dirs := map[string]bool{}
	for _, line := range strings.Split(string(diff)+string(untracked), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasSuffix(line, ".go") {
			continue
		}
		dirs[filepath.ToSlash(filepath.Dir(line))] = true
	}
	return dirs, nil
}

// reverseDeps widens the changed dirs with every package that directly imports
// one of them. Best-effort: when the import graph or module path cannot be
// built, the scope is just the changed dirs.
func reverseDeps(root string, changed map[string]bool) []string {
	scope := map[string]bool{}
	for d := range changed {
		scope[d] = true
	}
	module := modulePath(root)
	if module == "" {
		return keys(scope)
	}
	pg, err := analyzer.NewPackageGraph(root)
	if err != nil {
		return keys(scope)
	}

	changedImports := map[string]bool{}
	for d := range changed {
		if d == "." {
			changedImports[module] = true
			continue
		}
		changedImports[module+"/"+d] = true
	}
	for _, pkg := range pg.AllPackages() {
		for _, imp := range pkg.Imports {
			if !changedImports[imp] {
				continue
			}
			if rel := relDir(root, pkg.Dir); rel != "" {
				scope[rel] = true
			}
			break
		}
	}
	return keys(scope)
}

func relDir(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = dir
	}
	return filepath.ToSlash(rel)
}

// modulePath reads the module path from root's go.mod, or "".
func modulePath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
