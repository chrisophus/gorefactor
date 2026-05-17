package analyzer

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

// DiffAnalyzer analyzes code diffs and generates refactoring plans
type DiffAnalyzer struct{}

// NewDiffAnalyzer creates a new diff analyzer instance
func NewDiffAnalyzer() *DiffAnalyzer {
	return &DiffAnalyzer{}
}

// DiffHunk represents a single hunk in a diff
type DiffHunk struct {
	StartLine int      `json:"startLine"`
	EndLine   int      `json:"endLine"`
	Lines     []string `json:"lines"`
	Type      string   `json:"type"` // "added", "removed", "context"
}

// DiffFile represents a file in a diff
type DiffFile struct {
	Path     string      `json:"path"`
	Hunks    []*DiffHunk `json:"hunks"`
	Language string      `json:"language"`
}

// DiffAnalysis represents the analysis of a diff
type DiffAnalysis struct {
	Files   []*DiffFile                   `json:"files"`
	Summary string                        `json:"summary"`
	Changes []*Change                     `json:"changes"`
	Plan    *orchestrator.RefactoringPlan `json:"plan"`
}

// Change represents a detected change in the code
type Change struct {
	Type        string                 `json:"type"`
	File        string                 `json:"file"`
	Description string                 `json:"description"`
	StartLine   int                    `json:"startLine"`
	EndLine     int                    `json:"endLine"`
	Confidence  float64                `json:"confidence"`
	Details     map[string]interface{} `json:"details"`
}

// AnalyzeDiffFile analyzes a diff file and generates a refactoring plan
func (da *DiffAnalyzer) AnalyzeDiffFile(diffPath string) (*DiffAnalysis, error) {
	file, err := os.Open(diffPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open diff file: %w", err)
	}
	defer func() { _ = file.Close() }()

	return da.AnalyzeDiffReader(file)
}

// AnalyzeDiffString analyzes a diff string and generates a refactoring plan
func (da *DiffAnalyzer) AnalyzeDiffString(diffContent string) (*DiffAnalysis, error) {
	reader := strings.NewReader(diffContent)
	return da.AnalyzeDiffReader(reader)
}

type diffState struct {
	files       []*DiffFile
	currentFile *DiffFile
	currentHunk *DiffHunk
}

func createScanner(reader interface{}) (*bufio.Scanner, error) {
	switch r := reader.(type) {
	case *os.File:
		return bufio.NewScanner(r), nil
	case *strings.Reader:
		buf := make([]byte, r.Len())
		if len(buf) > 0 {
			if _, err := r.ReadAt(buf, 0); err != nil {
				return nil, fmt.Errorf("failed to read from reader: %w", err)
			}
		}
		return bufio.NewScanner(strings.NewReader(string(buf))), nil
	default:
		return nil, fmt.Errorf("unsupported reader type")
	}
}

// AnalyzeDiffReader analyzes a diff from a reader and generates a refactoring plan

// Convert strings.Reader to string content

// Parse file header

// Parse file path

// Parse hunk header

// Parse hunk content

// Add the last file and hunk

// Analyze the changes

// Consolidate related changes (e.g., multiple variable renames of the same variable)

// analyzeChanges analyzes the changes in the diff files
func (da *DiffAnalyzer) analyzeChanges(files []*DiffFile) []*Change {
	var changes []*Change

	for _, file := range files {
		if file.Language != "go" {
			continue // Only analyze Go files for now
		}

		for _, hunk := range file.Hunks {
			fileChanges := da.analyzeHunk(file.Path, hunk)
			changes = append(changes, fileChanges...)
		}
	}

	return changes
}

func (da *DiffAnalyzer) AnalyzeDiffReader(reader interface{}) (*DiffAnalysis, error) {
	scanner, err := createScanner(reader)
	if err != nil {
		return nil, err
	}
	state := &diffState{}
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "diff --git"):
			da.processDiffGit(state)
		case strings.HasPrefix(line, "--- a/") || strings.HasPrefix(line, "+++ b/"):
			da.processFilePath(state, line)
		case strings.HasPrefix(line, "@@"):
			da.processHunkHeader(state, line)
		default:
			if state.currentHunk != nil {
				state.currentHunk.Lines = append(state.currentHunk.Lines, line)
			}
		}
	}
	if state.currentFile != nil {
		if state.currentHunk != nil {
			state.currentFile.Hunks = append(state.currentFile.Hunks, state.currentHunk)
		}
		state.files = append(state.files, state.currentFile)
	}
	changes := da.analyzeChanges(state.files)
	changes = da.consolidateChanges(changes)
	summary := da.generateSummary(changes)
	plan := da.generateRefactoringPlan(changes)
	return &DiffAnalysis{
		Files:   state.files,
		Summary: summary,
		Changes: changes,
		Plan:    plan,
	}, nil
}
