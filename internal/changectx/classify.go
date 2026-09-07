package changectx

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// generatedHeaderRe matches the Go convention line that marks machine output.
// It is anchored at both ends on purpose: a substring test for "Code
// generated" also matches a file that merely discusses the convention, and
// hiding a hand-written file from the reviewer is the one failure this
// classifier must never make.
var generatedHeaderRe = regexp.MustCompile(
	`^\s*(?://+|/\*+|\*)?\s*Code generated\b.*\bDO NOT EDIT\.?\s*(?:\*/)?\s*$`)

// headerScanLines bounds how far into a file the header is looked for. The
// convention puts it above the package clause, so anything deeper is prose.
const headerScanLines = 40

// generatedNameSuffixes and generatedNamePrefixes are the Go toolchain and
// codegen naming conventions that mark output without a header.
var (
	generatedNameSuffixes = []string{".pb.go", ".gen.go", "_string.go"}
	generatedNamePrefixes = []string{"zz_generated.", "oas_"}
	generatedNames        = map[string]bool{"go.sum": true}
)

// lockfileNames are dependency lock files. They change constantly and carry no
// reviewable intent of their own.
var lockfileNames = map[string]bool{
	"go.sum": true, "package-lock.json": true, "yarn.lock": true,
	"pnpm-lock.yaml": true, "Cargo.lock": true, "Gemfile.lock": true,
	"poetry.lock": true, "composer.lock": true,
}

// migrationDirs are the directory names schema migrations conventionally live
// in. A migration reads differently from application code: it runs once, in
// order, against data that already exists.
var migrationDirs = map[string]bool{
	"migrations": true, "migration": true, "migrate": true,
}

// classify decides the manifest entry for one changed path. The order of the
// tests below is the precedence: the first answer that applies wins, so a
// generated lock file reports as a lock file and still carries the generated
// flag.
func classify(repo string, c change) (Class, bool) {
	p := filepath.ToSlash(c.path)
	base := path.Base(p)
	gen := isGeneratedName(base) || (!c.deleted() && hasGeneratedHeader(repo, p))

	switch {
	case hasSegment(p, "vendor") || strings.HasPrefix(p, "third_party/"):
		return ClassVendored, gen
	case lockfileNames[base]:
		return ClassLockfile, gen
	case isMigration(p, base):
		return ClassMigration, gen
	case gen:
		return ClassGenerated, true
	case strings.HasSuffix(base, "_test.go") || hasSegment(p, "testdata"):
		return ClassTest, gen
	case strings.HasSuffix(base, ".go"):
		return ClassSource, gen
	}
	return ClassOther, gen
}

func isGeneratedName(base string) bool {
	if generatedNames[base] {
		return true
	}
	for _, s := range generatedNameSuffixes {
		if strings.HasSuffix(base, s) {
			return true
		}
	}
	for _, p := range generatedNamePrefixes {
		if strings.HasPrefix(base, p) && strings.HasSuffix(base, ".go") {
			return true
		}
	}
	return false
}

// migrationNameRe matches the numbered-prefix convention most migration tools
// write, such as 000123_add_users.up.sql.
var migrationNameRe = regexp.MustCompile(`^[0-9]{3,}[_-].+\.(sql|go)$`)

func isMigration(p, base string) bool {
	for _, seg := range strings.Split(path.Dir(p), "/") {
		if migrationDirs[strings.ToLower(seg)] {
			return true
		}
	}
	return migrationNameRe.MatchString(base)
}

func hasSegment(p, want string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == want {
			return true
		}
	}
	return false
}

// hasGeneratedHeader reports whether the file carries the convention line
// above its first line of code. Reading stops at that first line, which is why
// a mention of the convention inside a function body cannot trigger it.
func hasGeneratedHeader(repo, rel string) bool {
	data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel)))
	if err != nil {
		return false
	}
	inBlock := false
	for i, line := range strings.Split(string(data), "\n") {
		if i >= headerScanLines {
			return false
		}
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		switch {
		case inBlock:
			if generatedHeaderRe.MatchString(t) {
				return true
			}
			if strings.Contains(t, "*/") {
				inBlock = false
			}
		case strings.HasPrefix(t, "/*"):
			if generatedHeaderRe.MatchString(t) {
				return true
			}
			if !strings.Contains(t[2:], "*/") {
				inBlock = true
			}
		case strings.HasPrefix(t, "//"):
			if generatedHeaderRe.MatchString(t) {
				return true
			}
		default:
			return false
		}
	}
	return false
}
