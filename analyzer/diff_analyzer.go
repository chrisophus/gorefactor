package analyzer

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorefactor/orchestrator"
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

// AnalyzeDiffReader analyzes a diff from a reader and generates a refactoring plan
func (da *DiffAnalyzer) AnalyzeDiffReader(reader interface{}) (*DiffAnalysis, error) {
	var scanner *bufio.Scanner

	switch r := reader.(type) {
	case *os.File:
		scanner = bufio.NewScanner(r)
	case *strings.Reader:
		// Convert strings.Reader to string content
		buf := make([]byte, r.Len())
		if len(buf) > 0 {
			if _, err := r.ReadAt(buf, 0); err != nil {
				return nil, fmt.Errorf("failed to read from reader: %w", err)
			}
		}
		scanner = bufio.NewScanner(strings.NewReader(string(buf)))
	default:
		return nil, fmt.Errorf("unsupported reader type")
	}

	var files []*DiffFile
	var currentFile *DiffFile
	var currentHunk *DiffHunk

	for scanner.Scan() {
		line := scanner.Text()

		// Parse file header
		if strings.HasPrefix(line, "diff --git") {
			if currentFile != nil {
				if currentHunk != nil {
					currentFile.Hunks = append(currentFile.Hunks, currentHunk)
				}
				files = append(files, currentFile)
			}
			currentFile = &DiffFile{}
			currentHunk = nil
			continue
		}

		// Parse file path
		if strings.HasPrefix(line, "--- a/") || strings.HasPrefix(line, "+++ b/") {
			if currentFile != nil && currentFile.Path == "" {
				path := strings.TrimPrefix(line, "--- a/")
				path = strings.TrimPrefix(path, "+++ b/")
				currentFile.Path = path
				currentFile.Language = da.detectLanguage(path)
			}
			continue
		}

		// Parse hunk header
		if strings.HasPrefix(line, "@@") {
			if currentHunk != nil && currentFile != nil {
				currentFile.Hunks = append(currentFile.Hunks, currentHunk)
			}
			currentHunk = da.parseHunkHeader(line)
			continue
		}

		// Parse hunk content
		if currentHunk != nil {
			currentHunk.Lines = append(currentHunk.Lines, line)
		}
	}

	// Add the last file and hunk
	if currentFile != nil {
		if currentHunk != nil {
			currentFile.Hunks = append(currentFile.Hunks, currentHunk)
		}
		files = append(files, currentFile)
	}

	// Analyze the changes
	changes := da.analyzeChanges(files)
	// Consolidate related changes (e.g., multiple variable renames of the same variable)
	changes = da.consolidateChanges(changes)
	summary := da.generateSummary(changes)
	plan := da.generateRefactoringPlan(changes)

	return &DiffAnalysis{
		Files:   files,
		Summary: summary,
		Changes: changes,
		Plan:    plan,
	}, nil
}

// parseHunkHeader parses a hunk header line
func (da *DiffAnalyzer) parseHunkHeader(line string) *DiffHunk {
	// Parse @@ -start,count +start,count @@ format
	re := regexp.MustCompile(`^@@ -(\d+),?(\d+)? \+(\d+),?(\d+)? @@`)
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

// detectLanguage detects the programming language from file extension
func (da *DiffAnalyzer) detectLanguage(path string) string {
	if strings.HasSuffix(path, ".go") {
		return "go"
	}
	if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".ts") {
		return "javascript"
	}
	if strings.HasSuffix(path, ".py") {
		return "python"
	}
	if strings.HasSuffix(path, ".java") {
		return "java"
	}
	return "unknown"
}

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

// analyzeAddedCode analyzes added code to detect patterns
func (da *DiffAnalyzer) analyzeAddedCode(filePath string, hunk *DiffHunk, addedLines []string) *Change {
	code := strings.Join(addedLines, "\n")

	// Detect method addition (check this before function addition)
	if da.isMethodAddition(code) {
		return &Change{
			Type:        "method_addition",
			File:        filePath,
			Description: "Added new method",
			StartLine:   hunk.StartLine,
			EndLine:     hunk.EndLine,
			Confidence:  0.9,
			Details: map[string]interface{}{
				"methodName":   da.extractMethodName(code),
				"receiverType": da.extractReceiverType(code),
				"code":         code,
			},
		}
	}

	// Detect function addition
	if da.isFunctionAddition(code) {
		return &Change{
			Type:        "function_addition",
			File:        filePath,
			Description: "Added new function",
			StartLine:   hunk.StartLine,
			EndLine:     hunk.EndLine,
			Confidence:  0.9,
			Details: map[string]interface{}{
				"functionName": da.extractFunctionName(code),
				"code":         code,
			},
		}
	}

	// Detect interface addition
	if da.isInterfaceAddition(code) {
		return &Change{
			Type:        "interface_addition",
			File:        filePath,
			Description: "Added new interface",
			StartLine:   hunk.StartLine,
			EndLine:     hunk.EndLine,
			Confidence:  0.9,
			Details: map[string]interface{}{
				"interfaceName": da.extractInterfaceName(code),
				"code":          code,
			},
		}
	}

	// Detect struct addition
	if da.isStructAddition(code) {
		return &Change{
			Type:        "struct_addition",
			File:        filePath,
			Description: "Added new struct",
			StartLine:   hunk.StartLine,
			EndLine:     hunk.EndLine,
			Confidence:  0.9,
			Details: map[string]interface{}{
				"structName": da.extractStructName(code),
				"code":       code,
			},
		}
	}

	// Detect code insertion
	return &Change{
		Type:        "code_insertion",
		File:        filePath,
		Description: "Inserted new code",
		StartLine:   hunk.StartLine,
		EndLine:     hunk.EndLine,
		Confidence:  0.7,
		Details: map[string]interface{}{
			"code": code,
		},
	}
}

// analyzeRemovedCode analyzes removed code to detect patterns
func (da *DiffAnalyzer) analyzeRemovedCode(filePath string, hunk *DiffHunk, removedLines []string) *Change {
	code := strings.Join(removedLines, "\n")

	// Detect function removal
	if da.isFunctionRemoval(code) {
		return &Change{
			Type:        "function_removal",
			File:        filePath,
			Description: "Removed function",
			StartLine:   hunk.StartLine,
			EndLine:     hunk.EndLine,
			Confidence:  0.9,
			Details: map[string]interface{}{
				"functionName": da.extractFunctionName(code),
				"code":         code,
			},
		}
	}

	// Detect code removal
	return &Change{
		Type:        "code_removal",
		File:        filePath,
		Description: "Removed code",
		StartLine:   hunk.StartLine,
		EndLine:     hunk.EndLine,
		Confidence:  0.7,
		Details: map[string]interface{}{
			"code": code,
		},
	}
}

// analyzeModifiedCode analyzes modified code to detect patterns
func (da *DiffAnalyzer) analyzeModifiedCode(filePath string, hunk *DiffHunk, modifiedLines [][]string) *Change {
	if len(modifiedLines) == 0 {
		return nil
	}

	// Each element in modifiedLines is a [old, new] pair
	pair := modifiedLines[0]
	if len(pair) < 2 {
		return nil
	}

	oldCode := pair[0]
	newCode := pair[1]

	// Detect variable renaming
	if da.isVariableRename(oldCode, newCode) {
		return &Change{
			Type:        "variable_rename",
			File:        filePath,
			Description: "Renamed variable",
			StartLine:   hunk.StartLine,
			EndLine:     hunk.EndLine,
			Confidence:  0.8,
			Details: map[string]interface{}{
				"oldName": da.extractVariableName(oldCode),
				"newName": da.extractVariableName(newCode),
				"oldCode": oldCode,
				"newCode": newCode,
			},
		}
	}

	// Detect function modification
	if da.isFunctionModification(oldCode, newCode) {
		return &Change{
			Type:        "function_modification",
			File:        filePath,
			Description: "Modified function",
			StartLine:   hunk.StartLine,
			EndLine:     hunk.EndLine,
			Confidence:  0.8,
			Details: map[string]interface{}{
				"functionName": da.extractFunctionName(oldCode),
				"oldCode":      oldCode,
				"newCode":      newCode,
			},
		}
	}

	// Generic code modification
	return &Change{
		Type:        "code_modification",
		File:        filePath,
		Description: "Modified code",
		StartLine:   hunk.StartLine,
		EndLine:     hunk.EndLine,
		Confidence:  0.6,
		Details: map[string]interface{}{
			"oldCode": oldCode,
			"newCode": newCode,
		},
	}
}

// Pattern detection methods
func (da *DiffAnalyzer) isFunctionAddition(code string) bool {
	return strings.Contains(code, "func ") && strings.Contains(code, "(") && strings.Contains(code, ")")
}

func (da *DiffAnalyzer) isMethodAddition(code string) bool {
	return strings.Contains(code, "func (") && strings.Contains(code, ") ") && !strings.Contains(code, "func ()")
}

func (da *DiffAnalyzer) isInterfaceAddition(code string) bool {
	return strings.Contains(code, "type ") && strings.Contains(code, "interface {")
}

func (da *DiffAnalyzer) isStructAddition(code string) bool {
	return strings.Contains(code, "type ") && strings.Contains(code, "struct {")
}

func (da *DiffAnalyzer) isFunctionRemoval(code string) bool {
	return strings.Contains(code, "func ") && strings.Contains(code, "(") && strings.Contains(code, ")")
}

func (da *DiffAnalyzer) isVariableRename(oldCode, newCode string) bool {
	// Trim whitespace for comparison
	oldCode = strings.TrimSpace(oldCode)
	newCode = strings.TrimSpace(newCode)

	// Check if codes differ in exactly one identifier
	oldIdents := da.extractIdentifiers(oldCode)
	newIdents := da.extractIdentifiers(newCode)

	// Count different identifiers
	oldSet := make(map[string]bool)
	newSet := make(map[string]bool)
	for _, id := range oldIdents {
		oldSet[id] = true
	}
	for _, id := range newIdents {
		newSet[id] = true
	}

	// If they have the same identifiers except for one difference, it's likely a rename
	oldOnly := []string{}
	newOnly := []string{}

	for id := range oldSet {
		if !newSet[id] {
			oldOnly = append(oldOnly, id)
		}
	}

	for id := range newSet {
		if !oldSet[id] {
			newOnly = append(newOnly, id)
		}
	}

	// Rename detected if exactly one identifier differs
	if len(oldOnly) == 1 && len(newOnly) == 1 {
		// Verify structure is similar by replacing the old identifier with the new one
		renamedCode := strings.ReplaceAll(oldCode, oldOnly[0], newOnly[0])
		if renamedCode == newCode {
			return true
		}
	}

	return false
}

// extractIdentifiers extracts all identifiers (variable names) from code
func (da *DiffAnalyzer) extractIdentifiers(code string) []string {
	re := regexp.MustCompile(`[a-zA-Z_][a-zA-Z0-9_]*`)
	matches := re.FindAllString(code, -1)
	// Deduplicate and return
	seen := make(map[string]bool)
	var result []string
	for _, m := range matches {
		if !seen[m] {
			result = append(result, m)
			seen[m] = true
		}
	}
	return result
}

func (da *DiffAnalyzer) isFunctionModification(oldCode, newCode string) bool {
	return strings.Contains(oldCode, "func ") && strings.Contains(newCode, "func ")
}

// Extraction methods
func (da *DiffAnalyzer) extractFunctionName(code string) string {
	re := regexp.MustCompile(`func\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\(`)
	matches := re.FindStringSubmatch(code)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (da *DiffAnalyzer) extractMethodName(code string) string {
	re := regexp.MustCompile(`func\s*\([^)]+\)\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\(`)
	matches := re.FindStringSubmatch(code)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (da *DiffAnalyzer) extractReceiverType(code string) string {
	re := regexp.MustCompile(`func\s*\([^*]*\*?([a-zA-Z_][a-zA-Z0-9_]*)\s*\)`)
	matches := re.FindStringSubmatch(code)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (da *DiffAnalyzer) extractInterfaceName(code string) string {
	re := regexp.MustCompile(`type\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+interface`)
	matches := re.FindStringSubmatch(code)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (da *DiffAnalyzer) extractStructName(code string) string {
	re := regexp.MustCompile(`type\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+struct`)
	matches := re.FindStringSubmatch(code)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (da *DiffAnalyzer) extractVariableName(code string) string {
	// First try assignment (x :=)
	re := regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)\s*:=`)
	matches := re.FindStringSubmatch(code)
	if len(matches) > 1 {
		return matches[1]
	}

	// Fall back to extracting the single identifier from the code
	// (useful for variable uses like fmt.Println(varName))
	idents := da.extractIdentifiers(code)
	if len(idents) == 1 {
		return idents[0]
	}
	// If multiple identifiers, return the last one (usually the variable in question)
	if len(idents) > 1 {
		return idents[len(idents)-1]
	}

	return ""
}

// consolidateChanges consolidates related changes (e.g., multiple renames of the same variable)
func (da *DiffAnalyzer) consolidateChanges(changes []*Change) []*Change {
	if len(changes) <= 1 {
		return changes
	}

	// Check if all changes are variable_rename with the same old/new names
	if allVariableRenames(changes) {
		// Get the first change's old/new names
		oldName := changes[0].Details["oldName"]
		newName := changes[0].Details["newName"]

		// Check if all renames are the same
		allSameRename := true
		for _, change := range changes {
			if change.Details["oldName"] != oldName || change.Details["newName"] != newName {
				allSameRename = false
				break
			}
		}

		if allSameRename && len(changes) > 1 {
			// Consolidate into single change with first and last line numbers
			consolidated := &Change{
				Type:        "variable_rename",
				File:        changes[0].File,
				Description: fmt.Sprintf("Renamed variable %v to %v", oldName, newName),
				StartLine:   changes[0].StartLine,
				EndLine:     changes[len(changes)-1].EndLine,
				Confidence:  0.8,
				Details: map[string]interface{}{
					"oldName":     oldName,
					"newName":     newName,
					"occurrences": len(changes),
				},
			}
			return []*Change{consolidated}
		}
	}

	return changes
}

// allVariableRenames checks if all changes are variable_rename type
func allVariableRenames(changes []*Change) bool {
	if len(changes) == 0 {
		return false
	}
	for _, change := range changes {
		if change.Type != "variable_rename" {
			return false
		}
	}
	return true
}

// generateSummary generates a summary of the changes
func (da *DiffAnalyzer) generateSummary(changes []*Change) string {
	if len(changes) == 0 {
		return "No changes detected"
	}

	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("Detected %d changes:\n", len(changes)))

	changeTypes := make(map[string]int)
	for _, change := range changes {
		changeTypes[change.Type]++
	}

	// Sort change types alphabetically for consistent output
	var types []string
	for changeType := range changeTypes {
		types = append(types, changeType)
	}
	sort.Strings(types)

	for _, changeType := range types {
		summary.WriteString(fmt.Sprintf("- %d %s\n", changeTypes[changeType], changeType))
	}

	return summary.String()
}

// generateRefactoringPlan generates a refactoring plan from the changes
func (da *DiffAnalyzer) generateRefactoringPlan(changes []*Change) *orchestrator.RefactoringPlan {
	plan := &orchestrator.RefactoringPlan{
		Version:     "1.0",
		Name:        "generated_from_diff",
		Description: "Refactoring plan generated from code diff analysis",
		Created:     time.Now(),
		Author:      "DiffAnalyzer",
		Operations:  []*orchestrator.RefactoringOperation{},
		Metadata: map[string]interface{}{
			"source":  "diff_analysis",
			"changes": len(changes),
		},
	}

	for _, change := range changes {
		operation := da.changeToOperation(change)
		if operation != nil {
			plan.Operations = append(plan.Operations, operation)
		}
	}

	return plan
}

// changeToOperation converts a change to a refactoring operation
func (da *DiffAnalyzer) changeToOperation(change *Change) *orchestrator.RefactoringOperation {
	switch change.Type {
	case "function_addition":
		return da.createInsertCodeOperation(change)
	case "method_addition":
		return da.createInsertCodeOperation(change)
	case "interface_addition":
		return da.createInsertCodeOperation(change)
	case "struct_addition":
		return da.createInsertCodeOperation(change)
	case "code_insertion":
		return da.createInsertCodeOperation(change)
	case "variable_rename":
		return da.createRenameVariableOperation(change)
	case "function_modification":
		return da.createExtractMethodOperation(change)
	default:
		return nil
	}
}

// createInsertCodeOperation creates an insert_code operation
func (da *DiffAnalyzer) createInsertCodeOperation(change *Change) *orchestrator.RefactoringOperation {
	code, _ := change.Details["code"].(string)

	return &orchestrator.RefactoringOperation{
		Type:        "insert_code",
		Description: change.Description,
		File:        change.File,
		Target: &orchestrator.TargetSpecification{
			StartLine: &change.StartLine,
		},
		Parameters: map[string]interface{}{
			"codeSnippet": code,
			"location": map[string]interface{}{
				"type": "at_end",
			},
		},
		Fallback: &orchestrator.FallbackStrategy{
			Type:        "skip",
			Description: "Skip if target not found",
		},
	}
}

// createRenameVariableOperation creates a rename_variable operation
func (da *DiffAnalyzer) createRenameVariableOperation(change *Change) *orchestrator.RefactoringOperation {
	oldName, _ := change.Details["oldName"].(string)
	newName, _ := change.Details["newName"].(string)

	return &orchestrator.RefactoringOperation{
		Type:        "rename_variable",
		Description: change.Description,
		File:        change.File,
		Target: &orchestrator.TargetSpecification{
			StartLine: &change.StartLine,
			EndLine:   &change.EndLine,
		},
		Parameters: map[string]interface{}{
			"oldName": oldName,
			"newName": newName,
		},
	}
}

// createExtractMethodOperation creates an extract_method operation
func (da *DiffAnalyzer) createExtractMethodOperation(change *Change) *orchestrator.RefactoringOperation {
	functionName, _ := change.Details["functionName"].(string)

	return &orchestrator.RefactoringOperation{
		Type:        "extract_method",
		Description: "Extract modified code into separate method",
		File:        change.File,
		Target: &orchestrator.TargetSpecification{
			FunctionName: functionName,
		},
		Parameters: map[string]interface{}{
			"methodName": fmt.Sprintf("extracted_%s", functionName),
		},
		Fallback: &orchestrator.FallbackStrategy{
			Type:        "skip",
			Description: "Skip if function not found",
		},
	}
}
