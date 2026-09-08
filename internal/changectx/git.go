package changectx

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

// gitCmd builds a git command targeted at root via -C, with the environment
// scrubbed of the repo-locating GIT_* variables. Under a git hook git exports
// GIT_DIR and GIT_INDEX_FILE pointing at the outer repository; inherited by a
// child process they redirect every diff and rev-parse at the wrong repo.
func gitCmd(root string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = analyzer.SanitizedGitEnv()
	return cmd
}

// gitOutput runs a git command and returns its stdout. Stderr is folded into
// the error so a caller reporting a note says what git said.
func gitOutput(root string, args ...string) (string, error) {
	cmd := gitCmd(root, args...)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return string(out), nil
}

// repoRoot returns the absolute top level of the work tree containing dir.
func repoRoot(dir string) (string, error) {
	out, err := gitOutput(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	top := strings.TrimSpace(out)
	if top == "" {
		return "", fmt.Errorf("git rev-parse --show-toplevel returned nothing for %s", dir)
	}
	return filepath.Clean(top), nil
}

// mergeBase resolves the point the change is measured from: the merge base of
// ref and HEAD. A ref with no common ancestor, and a repository with no HEAD,
// fall back to the ref itself so a first commit still produces an envelope.
func mergeBase(repo, ref string) (string, error) {
	if out, err := gitOutput(repo, "merge-base", ref, "HEAD"); err == nil {
		if sha := strings.TrimSpace(out); sha != "" {
			return sha, nil
		}
	}
	out, err := gitOutput(repo, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// change is one path the diff reports, with the git status letter that
// produced it. Untracked paths are reported as additions.
type change struct {
	path   string // repo-relative, forward slashes
	status string
}

func (c change) deleted() bool { return strings.HasPrefix(c.status, "D") }

// changedFiles lists every path that differs between base and the working
// tree, including files git does not track yet. A review that skips a new file
// because it was never added is worse than one that never ran, so a failure to
// list the untracked paths fails the call rather than returning a manifest
// that looks complete: the symbols and every expansion are derived from this
// list, and a silently dropped file leaves the envelope reading like a fully
// covered change.
func changedFiles(repo, base string) ([]change, error) {
	tracked, err := gitOutput(repo, "diff", "--name-status", "-z", base)
	if err != nil {
		return nil, err
	}
	byPath := map[string]change{}
	for _, c := range parseNameStatus(tracked) {
		byPath[c.path] = c
	}
	untracked, err := gitOutput(repo, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, p := range splitNUL(untracked) {
		p = filepath.ToSlash(p)
		if _, seen := byPath[p]; !seen {
			byPath[p] = change{path: p, status: "A"}
		}
	}
	out := make([]change, 0, len(byPath))
	for _, c := range byPath {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out, nil
}

// parseNameStatus reads the NUL-separated form of `git diff --name-status`.
// Rename and copy records carry two paths; the second one is where the code
// lives now, which is the one a reviewer reads.
func parseNameStatus(s string) []change {
	fields := splitNUL(s)
	var out []change
	for i := 0; i < len(fields); i++ {
		status := fields[i]
		if status == "" {
			continue
		}
		want := 1
		if status[0] == 'R' || status[0] == 'C' {
			want = 2
		}
		if i+want >= len(fields) {
			break
		}
		path := fields[i+want]
		i += want
		out = append(out, change{path: filepath.ToSlash(path), status: status})
	}
	return out
}

func splitNUL(s string) []string {
	var out []string
	for _, f := range strings.Split(s, "\x00") {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// lineRange is an inclusive 1-based span of lines in the working-tree file.
type lineRange struct {
	start int
	end   int
}

// hunkRanges returns the working-tree line spans the diff touches in path. A
// hunk that only deletes lines has no working-tree span of its own, so it is
// reported as the single line the deletion sits after, which is where the
// reviewer has to look.
func hunkRanges(repo, base, path string) ([]lineRange, error) {
	out, err := gitOutput(repo, "diff", "-U0", "--no-color", base, "--", path)
	if err != nil {
		return nil, err
	}
	var ranges []lineRange
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "@@") {
			continue
		}
		r, ok := parseHunkHeader(line)
		if ok {
			ranges = append(ranges, r)
		}
	}
	return mergeRanges(ranges), nil
}

// removedRanges returns the base-side spans a diff deletes from path.
//
// These are the lines that no longer exist, so they have no working-tree span
// and hunkRanges cannot report them. Their history is the interesting kind:
// the reason a line was added is the reason not to delete it, and a diff that
// only removes code carries none of that on its face.
func removedRanges(repo, base, path string) ([]lineRange, error) {
	out, err := gitOutput(repo, "diff", "-U0", "--no-color", base, "--", path)
	if err != nil {
		return nil, err
	}
	var ranges []lineRange
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "@@") {
			continue
		}
		if r, ok := parseRemovedHunkHeader(line); ok {
			ranges = append(ranges, r)
		}
	}
	return mergeRanges(ranges), nil
}

// parseRemovedHunkHeader reads the "-start,count" half. A count of zero means
// the hunk adds without removing, and there is nothing deleted to trace.
func parseRemovedHunkHeader(line string) (lineRange, bool) {
	i := strings.Index(line, "-")
	if i < 0 {
		return lineRange{}, false
	}
	rest := line[i+1:]
	if j := strings.IndexAny(rest, " @+"); j >= 0 {
		rest = rest[:j]
	}
	startStr, countStr, hasCount := strings.Cut(rest, ",")
	start, err := strconv.Atoi(startStr)
	if err != nil || start < 1 {
		return lineRange{}, false
	}
	count := 1
	if hasCount {
		count, err = strconv.Atoi(countStr)
		if err != nil {
			return lineRange{}, false
		}
	}
	if count == 0 {
		return lineRange{}, false
	}
	return lineRange{start: start, end: start + count - 1}, true
}

// logRemovedHistory traces a span that existed at base and does not now. The
// span is interpreted against base rather than the working tree, which is the
// only revision where those line numbers mean anything.
func logRemovedHistory(repo, base, path string, r lineRange, max int) (string, error) {
	spec := fmt.Sprintf("-L%d,%d:%s", r.start, r.end, path)
	return gitOutput(repo, "log", "--no-color", "--date=iso-strict",
		"-n", strconv.Itoa(max), spec, base)
}

// parseHunkHeader reads the "+start,count" half of a unified diff hunk header.
func parseHunkHeader(line string) (lineRange, bool) {
	i := strings.Index(line, "+")
	if i < 0 {
		return lineRange{}, false
	}
	rest := line[i+1:]
	if j := strings.IndexAny(rest, " @"); j >= 0 {
		rest = rest[:j]
	}
	startStr, countStr, hasCount := strings.Cut(rest, ",")
	start, err := strconv.Atoi(startStr)
	if err != nil {
		return lineRange{}, false
	}
	count := 1
	if hasCount {
		count, err = strconv.Atoi(countStr)
		if err != nil {
			return lineRange{}, false
		}
	}
	if count == 0 {
		if start < 1 {
			start = 1
		}
		return lineRange{start: start, end: start}, true
	}
	return lineRange{start: start, end: start + count - 1}, true
}

// mergeRanges sorts spans and folds overlapping or touching ones together, so
// one declaration spanning several hunks is asked about once.
func mergeRanges(in []lineRange) []lineRange {
	if len(in) == 0 {
		return nil
	}
	sorted := append([]lineRange(nil), in...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].start != sorted[j].start {
			return sorted[i].start < sorted[j].start
		}
		return sorted[i].end < sorted[j].end
	})
	out := []lineRange{sorted[0]}
	for _, r := range sorted[1:] {
		last := &out[len(out)-1]
		if r.start <= last.end+1 {
			if r.end > last.end {
				last.end = r.end
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// logLineHistory returns the recent history of a line span, capped at max
// revisions. The cap is what keeps this role cheap: a file rewritten fifty
// times would otherwise bury everything else in the envelope.
//
// The walk starts at rev rather than at HEAD, for the same reason
// logRemovedHistory does: the change under review sits at the tip, so a walk
// from HEAD answers "why is this line here" with the commit the reviewer is
// already reading. Walking from the base answers it with the history the
// change was made against. The span is the diff's working-tree side, so
// against the base it names the lines the hunk displaced rather than the
// added lines, which is the answerable half of the question: an added line
// has no history, and what a reviewer needs is the history of the code it
// was added into.
func logLineHistory(repo, rev, path string, r lineRange, max int) (string, error) {
	spec := fmt.Sprintf("-L%d,%d:%s", r.start, r.end, path)
	return gitOutput(repo, "log", "--no-color", "--date=iso-strict",
		"-n", strconv.Itoa(max), spec, rev)
}
