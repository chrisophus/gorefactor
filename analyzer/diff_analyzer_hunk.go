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

	_, _ = strconv.Atoi(matches[1]) // oldStart - not used
	_, _ = strconv.Atoi(matches[2]) // oldCount - not used
	newStart, _ := strconv.Atoi(matches[3])
	newCount, _ := strconv.Atoi(matches[4])

	return &DiffHunk{
		StartLine: newStart,
		EndLine:   newStart + newCount - 1,
		Lines:     []string{},
	}
}

// analyzeHunk analyzes a single hunk for changes
func (da *DiffAnalyzer) analyzeHunk(filePath string, hunk *DiffHunk) []*Change {
	var changes []*Change

	// Check for modifications first (take priority over add/remove)
	modifiedLines := da.getModifiedLines(hunk)
	if len(modifiedLines) > 0 {
		change := da.analyzeModifiedCode(filePath, hunk, modifiedLines)
		if change != nil {
			changes = append(changes, change)
			// If we found a modification, don't analyze as separate add/remove
			return changes
		}
	}

	// Analyze added lines
	addedLines := da.getAddedLines(hunk)
	if len(addedLines) > 0 {
		change := da.analyzeAddedCode(filePath, hunk, addedLines)
		if change != nil {
			changes = append(changes, change)
		}
	}

	// Analyze removed lines
	removedLines := da.getRemovedLines(hunk)
	if len(removedLines) > 0 {
		change := da.analyzeRemovedCode(filePath, hunk, removedLines)
		if change != nil {
			changes = append(changes, change)
		}
	}

	return changes
}

// getAddedLines extracts added lines from a hunk
func (da *DiffAnalyzer) getAddedLines(hunk *DiffHunk) []string {
	var added []string
	for _, line := range hunk.Lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added = append(added, strings.TrimPrefix(line, "+"))
		}
	}
	return added
}

// getRemovedLines extracts removed lines from a hunk
func (da *DiffAnalyzer) getRemovedLines(hunk *DiffHunk) []string {
	var removed []string
	for _, line := range hunk.Lines {
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			removed = append(removed, strings.TrimPrefix(line, "-"))
		}
	}
	return removed
}

// getModifiedLines extracts modified lines from a hunk
func (da *DiffAnalyzer) getModifiedLines(hunk *DiffHunk) [][]string {
	var modified [][]string
	var currentPair []string

	for _, line := range hunk.Lines {
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			if len(currentPair) == 0 {
				currentPair = append(currentPair, strings.TrimPrefix(line, "-"))
			}
		} else if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			if len(currentPair) == 1 {
				currentPair = append(currentPair, strings.TrimPrefix(line, "+"))
				modified = append(modified, currentPair)
				currentPair = []string{}
			}
		}
	}

	return modified
}
