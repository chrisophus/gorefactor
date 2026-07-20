package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// orphaned-config-path (harness-integrity plan item 3) is the config-path
// liveness check born from lesson 6: renaming dry_run_orchestrator.go
// silently broke a path-anchored golangci exemption, and nothing noticed
// until the un-exempted linter fired. Config that points at paths needs a
// liveness check: every .golangci.yml path regex and every lint-baseline
// file entry must still match something in the tree, or the entry is an
// orphaned exemption — silent scope creep in reverse. The scan walks the
// whole tree independently of lint's own walk exclusions, so an exemption
// for a deliberately-unwalked directory (e.g. fixtures) is not a false
// orphan.

type orphanedConfigPathRule struct{}

func (orphanedConfigPathRule) Name() string { return "orphaned-config-path" }

func (r orphanedConfigPathRule) Run(ctx LintContext) []lintIssue {
	root := ctx.Root
	if root == "" {
		root = "."
	}
	tree := listTreePaths(root)
	if len(tree) == 0 {
		return nil
	}
	var out []lintIssue
	out = append(out, golangciPathOrphans(root, tree)...)
	out = append(out, baselinePathOrphans(root)...)
	return out
}

// golangciConfigPaths is the minimal shape of a golangci-lint v2 config this
// check needs: the path-anchored exclusion regexes.
type golangciConfigPaths struct {
	Linters struct {
		Exclusions struct {
			Rules []struct {
				Path string `yaml:"path"`
			} `yaml:"rules"`
			Paths       []string `yaml:"paths"`
			PathsExcept []string `yaml:"paths-except"`
		} `yaml:"exclusions"`
	} `yaml:"linters"`
}

// golangciPathOrphans warns for every path regex in the repo's golangci
// config that matches zero paths in the tree.
func golangciPathOrphans(root string, tree []string) []lintIssue {
	cfgPath, data := readGolangciConfig(root)
	if data == nil {
		return nil
	}
	var cfg golangciConfigPaths
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	var patterns []string
	for _, r := range cfg.Linters.Exclusions.Rules {
		if r.Path != "" {
			patterns = append(patterns, r.Path)
		}
	}
	patterns = append(patterns, cfg.Linters.Exclusions.Paths...)
	patterns = append(patterns, cfg.Linters.Exclusions.PathsExcept...)

	var out []lintIssue
	seen := make(map[string]bool)
	for _, pat := range patterns {
		if seen[pat] {
			continue
		}
		seen[pat] = true
		re, err := regexp.Compile(pat)
		if err != nil {
			continue // golangci itself will reject it; not a liveness question
		}
		alive := false
		for _, p := range tree {
			if re.MatchString(p) {
				alive = true
				break
			}
		}
		if !alive && literalConfigPathExists(root, pat) {
			alive = true
		}
		if !alive {
			out = append(out, lintIssue{
				File:     cfgPath,
				Rule:     "orphaned-config-path",
				Severity: "warning",
				Message: fmt.Sprintf("path pattern %q matches no file in the tree — the exemption is orphaned; update or remove it (a rename of the exempted file un-exempts it silently)",
					pat),
			})
		}
	}
	return out
}

// literalConfigPathExists handles exclusions for trees intentionally pruned by
// listTreePaths, notably node_modules. Regex-shaped patterns still rely on the
// tree scan; only a literal repository-relative path is safe to stat directly.
func literalConfigPathExists(root, pattern string) bool {
	if pattern == "" || strings.ContainsAny(pattern, `\.+*?()|[]{}^$`) {
		return false
	}
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(pattern)))
	return err == nil
}

// baselinePathOrphans warns for every lint-baseline entry whose file no
// longer exists: its suppression covers nothing, and the finding it once
// suppressed will resurface as new elsewhere. Re-lock with --write-baseline.
func baselinePathOrphans(root string) []lintIssue {
	data, err := os.ReadFile(filepath.Join(root, defaultBaselinePath))
	if err != nil {
		return nil
	}
	var bl struct {
		Issues []struct {
			File string `json:"file"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(data, &bl); err != nil {
		return nil
	}
	missing := make(map[string]bool)
	for _, iss := range bl.Issues {
		path := trimLineColSuffix(iss.File)
		if path == "" || missing[path] {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, path)); os.IsNotExist(err) {
			missing[path] = true
		}
	}
	files := make([]string, 0, len(missing))
	for f := range missing {
		files = append(files, f)
	}
	sort.Strings(files)
	var out []lintIssue
	for _, f := range files {
		out = append(out, lintIssue{
			File:     defaultBaselinePath,
			Rule:     "orphaned-config-path",
			Severity: "warning",
			Message: fmt.Sprintf("baseline entry points at missing file %q — the suppression covers nothing; re-lock with `gorefactor lint . --write-baseline`",
				f),
		})
	}
	return out
}

var lineColSuffixRe = regexp.MustCompile(`^(.+\.go):\d+(?::\d+)?$`)

func trimLineColSuffix(file string) string {
	if m := lineColSuffixRe.FindStringSubmatch(file); m != nil {
		return m[1]
	}
	return file
}

// readGolangciConfig returns the repo-root golangci config path and bytes,
// or ("", nil) when none exists. Only the root config is checked: that is
// the one the doctor golangci stage and CI run with.
func readGolangciConfig(root string) (string, []byte) {
	for _, name := range []string{".golangci.yml", ".golangci.yaml"} {
		p := filepath.Join(root, name)
		if data, err := os.ReadFile(p); err == nil {
			return name, data
		}
	}
	return "", nil
}

// listTreePaths returns every file path under root, slash-separated and
// root-relative — the shape golangci matches its path regexes against.
// Unlike lint's Go-file walk it has no content exclusions (an exemption may
// legitimately point at a fixture directory the linter walk skips); only
// VCS and gorefactor state directories are pruned.
func listTreePaths(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == ".gorefactor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out
}
