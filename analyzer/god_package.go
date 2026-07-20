package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// God Package thresholds. A per-type god-object is a struct that does too
// much; a god package is the same failure one level up — a package that has
// accreted so many declarations across so many files (or reaches into so many
// sibling packages) that it has no single responsibility left. The thresholds
// are deliberately high: large-but-cohesive packages (a command-per-file CLI,
// a big analyzer) are common and healthy, so a package must be oversized on
// the primary axis (declaration count) AND sprawling or highly coupled on a
// second axis before it is flagged. Test files do not count toward any metric
// — a package's responsibility is its production surface.
const (
	// GodPackageMaxDecls is the top-level func+type declaration count above
	// which a package is a god-package candidate (the primary axis).
	GodPackageMaxDecls = 300
	// GodPackageMaxFiles is the non-test .go file count that, once the decl
	// threshold is passed, confirms the package is sprawling.
	GodPackageMaxFiles = 40
	// GodPackageMaxFanOut is the distinct intra-module imported-package count
	// that, once the decl threshold is passed, confirms the package is highly
	// coupled to the rest of the module (the coupling axis).
	GodPackageMaxFanOut = 12
)

// GodPackage describes a package that exceeds the god-package thresholds.
type GodPackage struct {
	Dir     string
	Files   int
	Decls   int
	FanOut  int
	Reasons []string
}

// DetectGodPackages reports packages whose production surface exceeds the
// god-package thresholds. modulePath is the module's import path (from go.mod);
// pass "" to disable the intra-module coupling axis (fan-out is then reported
// as zero and only the size axes apply). A package is flagged when its
// declaration count is over GodPackageMaxDecls AND it is either sprawling
// (files over GodPackageMaxFiles) or highly coupled (fan-out over
// GodPackageMaxFanOut).
func DetectGodPackages(files []string, modulePath string) []GodPackage {
	byDir := map[string][]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		dir := filepath.Dir(f)
		byDir[dir] = append(byDir[dir], f)
	}

	var out []GodPackage
	for dir, dirFiles := range byDir {
		decls := 0
		fanSet := map[string]bool{}
		for _, f := range dirFiles {
			content, err := readFileContent(f)
			if err != nil {
				continue
			}
			fset := token.NewFileSet()
			af, err := parser.ParseFile(fset, f, content, parser.SkipObjectResolution)
			if err != nil {
				continue
			}
			collectIntraModuleImports(af, modulePath, fanSet)
			decls += countTopLevelDecls(af)
		}
		gp := GodPackage{Dir: dir, Files: len(dirFiles), Decls: decls, FanOut: len(fanSet)}
		if reasons := godPackageReasons(gp); len(reasons) > 0 {
			gp.Reasons = reasons
			out = append(out, gp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out
}

// godPackageReasons returns the human-readable threshold breaches that make gp
// a god package, or nil when gp is under threshold. The decl axis is required;
// at least one of the sprawl/coupling axes must also breach.
func godPackageReasons(gp GodPackage) []string {
	if gp.Decls <= GodPackageMaxDecls {
		return nil
	}
	var secondary []string
	if gp.Files > GodPackageMaxFiles {
		secondary = append(secondary, strconv.Itoa(gp.Files)+" files (>"+strconv.Itoa(GodPackageMaxFiles)+")")
	}
	if gp.FanOut > GodPackageMaxFanOut {
		secondary = append(secondary, strconv.Itoa(gp.FanOut)+" intra-module imports (>"+strconv.Itoa(GodPackageMaxFanOut)+")")
	}
	if len(secondary) == 0 {
		return nil
	}
	reasons := []string{strconv.Itoa(gp.Decls) + " top-level declarations (>" + strconv.Itoa(GodPackageMaxDecls) + ")"}
	return append(reasons, secondary...)
}

// countTopLevelDecls counts top-level function and type declarations in one
// parsed file. Nested funcs and locally-declared types are not top-level and
// do not count.
func countTopLevelDecls(af *ast.File) int {
	count := 0
	for _, d := range af.Decls {
		switch g := d.(type) {
		case *ast.FuncDecl:
			count++
		case *ast.GenDecl:
			if g.Tok == token.TYPE {
				count += len(g.Specs)
			}
		}
	}
	return count
}

// collectIntraModuleImports adds to set every import path in af that belongs
// to modulePath (the same module), excluding a package's self-import edge is
// unnecessary because a file never imports its own package.
func collectIntraModuleImports(af *ast.File, modulePath string, set map[string]bool) {
	if modulePath == "" {
		return
	}
	for _, imp := range af.Imports {
		if imp.Path == nil {
			continue
		}
		p := strings.Trim(imp.Path.Value, "\"")
		if p == modulePath || strings.HasPrefix(p, modulePath+"/") {
			set[p] = true
		}
	}
}
