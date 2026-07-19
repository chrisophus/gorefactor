package analyzer

import (
	"regexp"
	"strconv"
	"strings"
)

var parseHunkHeaderRe = regexp.MustCompile(`^@@ -(\d+),?(\d+)? \+(\d+),?(\d+)? @@`)

func (da *DiffAnalyzer) processDiffGit(state *diffState) {
	if state.currentFile != nil {
		if state.currentHunk != nil {
			state.currentFile.Hunks = append(state.currentFile.Hunks, state.currentHunk)
		}
		state.files = append(state.files, state.currentFile)
	}
	state.currentFile = &DiffFile{}
	state.currentHunk = nil
}

func (da *DiffAnalyzer) processFilePath(state *diffState, line string) {
	if state.currentFile != nil && state.currentFile.Path == "" {
		path := strings.TrimPrefix(line, "--- a/")
		path = strings.TrimPrefix(path, "+++ b/")
		state.currentFile.Path = path
		state.currentFile.Language = da.detectLanguage(path)
	}
}

func (da *DiffAnalyzer) processHunkHeader(state *diffState, line string) {
	if state.currentHunk != nil && state.currentFile != nil {
		state.currentFile.Hunks = append(state.currentFile.Hunks, state.currentHunk)
	}
	state.currentHunk = da.parseHunkHeader(line)
}

// parseHunkHeader parses a hunk header line
func (da *DiffAnalyzer) parseHunkHeader(line string) *DiffHunk {
	// Parse @@ -start,count +start,count @@ format
	re := parseHunkHeaderRe
	matches := re.FindStringSubmatch(line)
	if len(matches) < 5 {
		return &DiffHunk{}
	}

	oldStart, _ := strconv.Atoi(matches[1])
	oldCount, _ := strconv.Atoi(matches[2])
	if matches[2] == "" {
		oldCount = 1
	}
	newStart, _ := strconv.Atoi(matches[3])
	newCount, _ := strconv.Atoi(matches[4])
	if matches[4] == "" {
		newCount = 1
	}

	return &DiffHunk{
		StartLine:    newStart,
		EndLine:      newStart + newCount - 1,
		OldStartLine: oldStart,
		OldEndLine:   oldStart + oldCount - 1,
		Lines:        []string{},
	}

}

// analyzeHunk analyzes a single hunk for changes
func (da *DiffAnalyzer) analyzeHunk(filePath string, hunk *DiffHunk) []*Change {
	var changes []*Change
	for _, run := range splitHunkRuns(hunk) {
		runHunk := run.asHunk()
		var change *Change
		switch {
		case len(run.oldLines) > 0 && len(run.newLines) > 0:
			change = da.analyzeModifiedCode(filePath, runHunk, run.oldLines, run.newLines)
		case len(run.newLines) > 0:
			change = da.analyzeAddedCode(filePath, runHunk, run.newLines)
		case len(run.oldLines) > 0:
			change = da.analyzeRemovedCode(filePath, runHunk, run.oldLines)
		}
		if change != nil {
			changes = append(changes, change)
		}
	}
	return changes

}

type hunkRun struct {
	oldLines []string
	newLines []string
	oldStart int
	newStart int
}

func (r hunkRun) asHunk() *DiffHunk {
	newEnd := r.newStart
	if n := len(r.newLines); n > 0 {
		newEnd = r.newStart + n - 1
	}
	oldEnd := r.oldStart
	if n := len(r.oldLines); n > 0 {
		oldEnd = r.oldStart + n - 1
	}
	return &DiffHunk{
		StartLine:    r.newStart,
		EndLine:      newEnd,
		OldStartLine: r.oldStart,
		OldEndLine:   oldEnd,
	}
}

func splitHunkRuns(hunk *DiffHunk) []hunkRun {
	newLine := hunk.StartLine
	oldLine := hunk.OldStartLine
	if oldLine == 0 {
		oldLine = hunk.StartLine
	}
	var runs []hunkRun
	var cur *hunkRun
	flush := func() {
		if cur != nil {
			runs = append(runs, *cur)
			cur = nil
		}
	}
	start := func() {
		if cur == nil {
			cur = &hunkRun{oldStart: oldLine, newStart: newLine}
		}
	}
	for _, line := range hunk.Lines {
		switch {
		case strings.HasPrefix(line, "\\"):

		case strings.HasPrefix(line, "-"):
			start()
			cur.oldLines = append(cur.oldLines, strings.TrimPrefix(line, "-"))
			oldLine++
		case strings.HasPrefix(line, "+"):
			start()
			cur.newLines = append(cur.newLines, strings.TrimPrefix(line, "+"))
			newLine++
		default:
			flush()
			oldLine++
			newLine++
		}
	}
	flush()
	return runs
}
