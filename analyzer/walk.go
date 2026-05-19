package analyzer

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// MinDuplicateImpactScore is the default threshold for reporting duplicate blocks
// in lint output. Blocks below this score are still returned by FindDuplicateBlocks
// but are typically filtered by callers (see cmd/gorefactor/cmd_lint_duplicates.go).
const MinDuplicateImpactScore = 5

// WalkOptions configures which directories and files are included when walking
// a module tree for analysis (lint, duplicate detection, dead code, etc.).
type WalkOptions struct {
	// SkipGeneratedGo skips *.gen.go and *_gen.go files (codegen convention).
	// Default true in DefaultWalkOptions.
	SkipGeneratedGo bool
	// ExtraSkipDirSegments prunes directories whose slash-normalized path contains
	// any of these segments (e.g. "api/marketplace-servergen", "internal/data/db").
	ExtraSkipDirSegments []string
}

// DefaultWalkOptions returns walk settings suitable for hand-authored Go in most repos:
// skip vendor/.git/node_modules, hidden dirs, and common generated file suffixes.
func DefaultWalkOptions() WalkOptions {
	return WalkOptions{SkipGeneratedGo: true}
}

// ShouldSkipDir reports whether a directory path should be pruned during walks.
func ShouldSkipDir(path string, opts WalkOptions) bool {
	p := filepath.ToSlash(path)
	base := filepath.Base(p)
	if strings.HasPrefix(base, ".") && base != "." && base != ".." {
		return true
	}
	switch base {
	case "vendor", "node_modules", ".git", ".gorefactor":
		return true
	}
	for _, seg := range opts.ExtraSkipDirSegments {
		if pathHasDirSegment(p, seg) || strings.HasSuffix(p, "/"+seg) || p == seg {
			return true
		}
	}
	return false
}

// ShouldSkipFile reports whether a .go file path should be excluded from analysis.
func ShouldSkipFile(path string, opts WalkOptions) bool {
	if !strings.HasSuffix(path, ".go") {
		return true
	}
	if opts.SkipGeneratedGo && isGeneratedGoFilename(path) {
		return true
	}
	return false
}

func isGeneratedGoFilename(path string) bool {
	return strings.HasSuffix(path, ".gen.go") || strings.HasSuffix(path, "_gen.go")
}

// pathHasDirSegment reports whether seg appears as a directory segment in p.
func pathHasDirSegment(p, seg string) bool {
	if p == seg {
		return true
	}
	if strings.HasPrefix(p, seg+"/") {
		return true
	}
	return strings.Contains(p, "/"+seg+"/")
}

// WalkGoFiles walks root and returns non-skipped .go file paths.
func WalkGoFiles(root string, opts WalkOptions) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if ShouldSkipDir(path, opts) {
				return filepath.SkipDir
			}
			return nil
		}
		if ShouldSkipFile(path, opts) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

// GroupFilesByDir groups file paths by their containing directory (Go package dir).
func GroupFilesByDir(files []string) map[string][]string {
	out := make(map[string][]string)
	for _, f := range files {
		dir := filepath.Dir(f)
		out[dir] = append(out[dir], f)
	}
	return out
}

// findGoFiles recursively finds Go files under dirPath using DefaultWalkOptions.
// Prefer WalkGoFiles when callers need custom ExtraSkipDirSegments.
func findGoFiles(dirPath string) ([]string, error) {
	return WalkGoFiles(dirPath, DefaultWalkOptions())
}
