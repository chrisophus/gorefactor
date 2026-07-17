package doctor

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

// filterFindings is the merge layer's uniform skip pass (plan decision 14):
// generated files and .gorefactor.yaml walk: skip rules apply to every
// substrate's findings, because deadcode/apidiff have no native skip configs
// and golangci's is separate. Findings without a file (e.g. apidiff package
// deltas) always pass through.
func filterFindings(findings []Finding, root string, walk analyzer.WalkOptions) []Finding {
	rootPrefix := ""
	if abs, err := filepath.Abs(root); err == nil {
		rootPrefix = abs + string(filepath.Separator)
	}
	genCache := map[string]bool{}
	out := findings[:0]
	for _, f := range findings {
		if f.File == "" || !strings.HasSuffix(f.File, ".go") {
			out = append(out, f)
			continue
		}
		rel := f.File
		if filepath.IsAbs(rel) {
			if r, err := filepath.Rel(root, rel); err == nil {
				rel = r
			}
		}
		abs := filepath.Join(root, rel)
		if analyzer.ShouldSkipFile(rel, walk) || skipDirAnywhere(filepath.Dir(rel), walk) {
			continue
		}

		if isGeneratedFile(abs, genCache) {
			continue
		}
		f.File = filepath.ToSlash(rel)
		if rootPrefix != "" {
			// Messages embed file paths (duplicate-block cross-references,
			// funcorder locations); strip the root prefix so fingerprints
			// match between the working tree and the baseline worktree.
			f.Message = strings.ReplaceAll(f.Message, rootPrefix, "")
		}
		out = append(out, f)
	}
	return out
}

func skipDirAnywhere(relDir string, walk analyzer.WalkOptions) bool {
	relDir = filepath.ToSlash(relDir)
	if relDir == "." || relDir == "" {
		return false
	}
	prefix := ""
	for _, seg := range strings.Split(relDir, "/") {
		prefix = filepath.Join(prefix, seg)
		if analyzer.ShouldSkipDir(prefix, walk) {
			return true
		}
	}
	return false
}

func normalizeFindings(findings []Finding, root string, walk analyzer.WalkOptions) []Finding {
	out := filterFindings(findings, root, walk)
	for i := range out {
		out[i].Fingerprint = fingerprint(out[i])
	}
	return out
}

// isGeneratedFile reports whether the file carries the standard
// "^// Code generated ... DO NOT EDIT.$" header (https://go.dev/s/generatedcode),
// checking the lines before the package clause. Results are memoized per run.
func isGeneratedFile(path string, cache map[string]bool) bool {
	if v, ok := cache[path]; ok {
		return v
	}
	gen := hasGeneratedHeader(path)
	cache[path] = gen
	return gen
}

func hasGeneratedHeader(path string) bool {
	fh, err := os.Open(path)
	if err != nil {
		return false
	}
	defer fh.Close()
	scanner := bufio.NewScanner(fh)
	for i := 0; scanner.Scan() && i < 50; i++ {
		line := scanner.Text()
		if strings.HasPrefix(line, "package ") {
			return false
		}
		if strings.HasPrefix(line, "// Code generated ") && strings.HasSuffix(line, " DO NOT EDIT.") {
			return true
		}
	}
	return false
}
