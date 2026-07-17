package doctor

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
	"golang.org/x/mod/modfile"
)

// ModTidy is the dependency-hygiene substrate (design plan step 4, the
// bundle-size analog): `go mod tidy -diff` reports requirements the module is
// missing or no longer needs, and an imports scan flags direct dependencies
// used from exactly one file. Module tidiness has no package scope, but the
// check is cheap enough to run on every gate run, so it is marked
// scope-capable and simply ignores ScopeDirs.
type ModTidy struct{}

// Info implements Substrate. Not gating: dependency hygiene is dead-category,
// warning severity.
func (ModTidy) Info() SubstrateInfo {
	return SubstrateInfo{Name: "modtidy", ScopeCapable: true}
}

// Run implements Substrate.
func (ModTidy) Run(ctx RunContext) ([]Finding, error) {
	if _, err := os.Stat(filepath.Join(ctx.Root, "go.mod")); err != nil {
		return nil, unavailablef("no go.mod in %s", ctx.Root)
	}
	findings, err := tidyDiffFindings(ctx.Root)
	if err != nil {
		return nil, err
	}
	single, err := singleUseDependencyFindings(ctx.Root)
	if err != nil {
		return nil, err
	}
	return append(findings, single...), nil
}

// tidyDiffFindings runs `go mod tidy -diff` (read-only; exits 1 with a diff
// when the module is untidy) and maps go.mod requirement changes to findings.
func tidyDiffFindings(root string) ([]Finding, error) {
	cmd := exec.Command("go", "mod", "tidy", "-diff")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	if runErr != nil && len(bytes.TrimSpace(out)) == 0 {
		// No diff on stdout means the command itself failed (network,
		// toolchain), not that the module is untidy.
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		return nil, unavailablef("go mod tidy -diff failed: %s", msg)
	}
	return parseTidyDiff(out), nil
}

// parseTidyDiff extracts go.mod requirement changes from the unified diff.
// go.sum churn is collapsed into the same finding set implicitly: a go.sum
// that changes without go.mod lines is reported once.
func parseTidyDiff(out []byte) []Finding {
	var findings []Finding
	inGoMod := false
	sumChanged := false
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "--- "):
			inGoMod = strings.Contains(line, "go.mod")
			sumChanged = sumChanged || strings.Contains(line, "go.sum")
		case !strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "-"):
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
		case inGoMod:
			if f, ok := tidyDiffFinding(line); ok {
				findings = append(findings, f)
			}
		}
	}
	if len(findings) == 0 && sumChanged {
		findings = append(findings, Finding{
			File:     "go.sum",
			Rule:     "modtidy/untidy",
			Category: CategoryDead,
			Message:  "go.sum is not tidy",
			FixCmd:   "go mod tidy",
		})
	}
	return findings

}

func tidyDiffFinding(line string) (Finding, bool) {
	change := strings.TrimSpace(line[1:])
	switch change {
	case "", "(", ")", "require (", "require":
		return Finding{}, false
	}
	verb := "missing requirement"
	if line[0] == '-' {
		verb = "unneeded requirement"
	}
	return Finding{
		File:     "go.mod",
		Rule:     "modtidy/untidy",
		Category: CategoryDead,
		Message:  fmt.Sprintf("go.mod is not tidy: %s %q", verb, change),
		FixCmd:   "go mod tidy",
	}, true
}

// singleUseDependencyFindings flags direct requirements imported from exactly
// one file: a whole module pulled in for one call site is worth a look —
// advisory only (info), because a single use can still be load-bearing.
func singleUseDependencyFindings(root string) ([]Finding, error) {
	direct, err := directRequirements(root)
	if err != nil || len(direct) == 0 {
		return nil, err
	}
	files, err := analyzer.WalkGoFiles(root, analyzer.DefaultWalkOptions())
	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}
	usedIn := dependencyUsage(files, direct)
	var findings []Finding
	for modPath, inFiles := range usedIn {
		if len(inFiles) != 1 {
			continue
		}
		var file string
		for f := range inFiles {
			file = f
		}
		rel, rerr := filepath.Rel(root, file)
		if rerr != nil {
			rel = file
		}
		findings = append(findings, Finding{
			File:     filepath.ToSlash(rel),
			Rule:     "modtidy/single-use-dependency",
			Category: CategoryDead,
			Severity: SeverityInfo,
			Message:  fmt.Sprintf("direct dependency %s is imported only here — check it earns its place", modPath),
		})
	}
	return findings, nil

}

func directRequirements(root string) (map[string]bool, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("read go.mod: %w", err)
	}
	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, fmt.Errorf("parse go.mod: %w", err)
	}
	direct := map[string]bool{}
	for _, req := range mod.Require {
		if !req.Indirect {
			direct[req.Mod.Path] = true
		}
	}
	return direct, nil
}

func dependencyUsage(files []string, direct map[string]bool) map[string]map[string]bool {
	usedIn := map[string]map[string]bool{}
	fset := token.NewFileSet()
	for _, f := range files {
		astFile, perr := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if perr != nil {
			continue
		}
		for _, imp := range astFile.Imports {
			path, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil {
				continue
			}
			for modPath := range direct {
				if path == modPath || strings.HasPrefix(path, modPath+"/") {
					if usedIn[modPath] == nil {
						usedIn[modPath] = map[string]bool{}
					}
					usedIn[modPath][f] = true
				}
			}
		}
	}
	return usedIn
}
