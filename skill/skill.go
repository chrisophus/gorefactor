// Command skill-refactor provides a Claude Code skill interface for gorefactor operations.
// This tool makes refactoring decisions intelligently and applies them efficiently.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// SkillOutput represents the structured output for Claude Code
type SkillOutput struct {
	Success         bool                       `json:"success"`
	Operation       string                     `json:"operation"`
	File            string                     `json:"file"`
	Changes         []Change                   `json:"changes"`
	Recommendations []ExtractionRecommendation `json:"recommendations,omitempty"`
	Message         string                     `json:"message"`
	Details         map[string]interface{}     `json:"details,omitempty"`
}

// Change represents a code change made
type Change struct {
	Type       string `json:"type"`
	StartLine  int    `json:"startLine"`
	EndLine    int    `json:"endLine"`
	MethodName string `json:"methodName,omitempty"`
	Variables  struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"variables,omitempty"`
}

// ExtractionRecommendation represents a recommended extraction
type ExtractionRecommendation struct {
	StartLine      int      `json:"startLine"`
	EndLine        int      `json:"endLine"`
	Complexity     int      `json:"complexity"`
	StatementCount int      `json:"statementCount"`
	ReadVars       []string `json:"readVars"`
	WriteVars      []string `json:"writeVars"`
	Extractable    bool     `json:"extractable"`
	Priority       int      `json:"priority"` // 1-10, higher is better
	SuggestedName  string   `json:"suggestedName"`
}

var (
	gorefactorPath = flag.String("gorefactor", "./gorefactor", "Path to gorefactor binary")
	outputJSON     = flag.Bool("json", true, "Output as JSON")
)

func main() {
	flag.Parse()

	if flag.NArg() < 1 {
		printUsage()
		os.Exit(1)
	}

	cmd := flag.Arg(0)
	args := flag.Args()[1:]

	var output SkillOutput
	var err error

	switch cmd {
	case "analyze":
		output, err = handleAnalyze(args)
	case "refactor":
		output, err = handleRefactor(args)
	case "extract":
		output, err = handleExtract(args)
	case "suggest":
		output, err = handleSuggest(args)
	default:
		output = SkillOutput{
			Success: false,
			Message: fmt.Sprintf("Unknown command: %s", cmd),
		}
	}

	if err != nil {
		output.Success = false
		output.Message = err.Error()
	}

	if *outputJSON {
		jsonOut, _ := json.MarshalIndent(output, "", "  ")
		fmt.Println(string(jsonOut))
	} else {
		fmt.Println(output.Message)
	}

	if !output.Success {
		os.Exit(1)
	}
}

// handleAnalyze analyzes a Go file for refactoring opportunities
func handleAnalyze(args []string) (SkillOutput, error) {
	if len(args) < 1 {
		return SkillOutput{Success: false, Message: "Usage: skill analyze <file>"}, nil
	}

	file := args[0]

	// Run gorefactor recommend
	cmd := exec.Command(*gorefactorPath, "recommend", file,
		"--min-complexity", "2",
		"--max-complexity", "15",
		"--min-statements", "3",
		"--max-statements", "40")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return SkillOutput{
			Success: false,
			Message: fmt.Sprintf("Failed to analyze: %v", err),
			Details: map[string]interface{}{
				"stderr": string(output),
			},
		}, nil
	}

	// Parse recommendations (top-level array) into typed struct
	type BlockInfo struct {
		StartLine      int      `json:"startLine"`
		EndLine        int      `json:"endLine"`
		Complexity     int      `json:"complexity"`
		StatementCount int      `json:"statementCount"`
		ReadVars       []string `json:"readVars"`
		WriteVars      []string `json:"writeVars"`
		Extractable    bool     `json:"extractable"`
		IsExtractable  bool     `json:"isExtractable"`
	}

	var blocks []BlockInfo
	if err := json.Unmarshal(output, &blocks); err != nil {
		return SkillOutput{
			Success: false,
			Message: fmt.Sprintf("Failed to parse results: %v", err),
			Details: map[string]interface{}{
				"output": string(output),
			},
		}, nil
	}

	// Convert to recommendations
	recommendations := []ExtractionRecommendation{}
	for _, block := range blocks {
		// Use isExtractable if extractable is false (handle both field names)
		isExtractable := block.Extractable || block.IsExtractable

		rec := ExtractionRecommendation{
			StartLine:      block.StartLine,
			EndLine:        block.EndLine,
			Complexity:     block.Complexity,
			StatementCount: block.StatementCount,
			ReadVars:       block.ReadVars,
			WriteVars:      block.WriteVars,
			Extractable:    isExtractable,
		}

		// Calculate priority (1-10)
		rec.Priority = calculatePriority(rec)
		rec.SuggestedName = suggestMethodName(rec)

		recommendations = append(recommendations, rec)
	}

	// Sort by priority
	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].Priority > recommendations[j].Priority
	})

	return SkillOutput{
		Success:         true,
		Operation:       "analyze",
		File:            file,
		Recommendations: recommendations,
		Message:         fmt.Sprintf("Found %d extraction candidates", len(recommendations)),
	}, nil
}

// handleRefactor applies intelligent refactoring to a file
func handleRefactor(args []string) (SkillOutput, error) {
	if len(args) < 1 {
		return SkillOutput{Success: false, Message: "Usage: skill refactor <file>"}, nil
	}

	file := args[0]
	maxExtractions := 3

	if len(args) > 1 {
		if n, err := strconv.Atoi(args[1]); err == nil {
			maxExtractions = n
		}
	}

	changes := []Change{}
	applied := 0

	// Apply refactorings iteratively, re-analyzing after each successful extraction
	// This ensures line numbers remain accurate as the file changes
	for applied < maxExtractions {
		// Analyze current state
		analyzeOutput, err := handleAnalyze([]string{file})
		if err != nil {
			break
		}

		if len(analyzeOutput.Recommendations) == 0 {
			break // No more opportunities
		}

		// Find the highest priority extractable recommendation
		var bestRec *ExtractionRecommendation
		for i := range analyzeOutput.Recommendations {
			rec := &analyzeOutput.Recommendations[i]
			if rec.Extractable && (bestRec == nil || rec.Priority > bestRec.Priority) {
				bestRec = rec
			}
		}

		if bestRec == nil {
			break // No extractable recommendations
		}

		// Apply extraction
		methodName := bestRec.SuggestedName
		cmd := exec.Command(*gorefactorPath, "extract", file,
			strconv.Itoa(bestRec.StartLine),
			strconv.Itoa(bestRec.EndLine),
			methodName)

		_, err = cmd.CombinedOutput()
		if err != nil {
			// Skip failed extraction and continue
			continue
		}

		changes = append(changes, Change{
			Type:       "extract_method",
			StartLine:  bestRec.StartLine,
			EndLine:    bestRec.EndLine,
			MethodName: methodName,
		})

		applied++
	}

	return SkillOutput{
		Success:   true,
		Operation: "refactor",
		File:      file,
		Changes:   changes,
		Message:   fmt.Sprintf("Applied %d refactorings", len(changes)),
	}, nil
}

// handleExtract extracts a specific code block
func handleExtract(args []string) (SkillOutput, error) {
	if len(args) < 4 {
		return SkillOutput{
			Success: false,
			Message: "Usage: skill extract <file> <startLine> <endLine> <methodName>",
		}, nil
	}

	file := args[0]
	startLineStr := args[1]
	endLineStr := args[2]
	methodName := args[3]

	// Validate line numbers are valid integers
	start, err := strconv.Atoi(startLineStr)
	if err != nil {
		return SkillOutput{
			Success: false,
			Message: fmt.Sprintf("Invalid startLine: %q is not a valid integer", startLineStr),
		}, nil
	}

	end, err := strconv.Atoi(endLineStr)
	if err != nil {
		return SkillOutput{
			Success: false,
			Message: fmt.Sprintf("Invalid endLine: %q is not a valid integer", endLineStr),
		}, nil
	}

	// Validate line range
	if start <= 0 || end <= 0 || start > end {
		return SkillOutput{
			Success: false,
			Message: fmt.Sprintf("Invalid line range: startLine=%d, endLine=%d (must be positive with start <= end)", start, end),
		}, nil
	}

	cmd := exec.Command(*gorefactorPath, "extract", file, startLineStr, endLineStr, methodName)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return SkillOutput{
			Success: false,
			Message: fmt.Sprintf("Extraction failed: %v", err),
			Details: map[string]interface{}{
				"stderr": string(output),
			},
		}, nil
	}

	return SkillOutput{
		Success:   true,
		Operation: "extract",
		File:      file,
		Changes: []Change{
			{
				Type:       "extract_method",
				StartLine:  start,
				EndLine:    end,
				MethodName: methodName,
			},
		},
		Message: fmt.Sprintf("Extracted method %s", methodName),
	}, nil
}

// handleSuggest provides refactoring suggestions without applying them
func handleSuggest(args []string) (SkillOutput, error) {
	if len(args) < 1 {
		return SkillOutput{Success: false, Message: "Usage: skill suggest <file>"}, nil
	}

	return handleAnalyze(args)
}

// calculatePriority calculates extraction priority (1-10, higher is better)
func calculatePriority(rec ExtractionRecommendation) int {
	if !rec.Extractable {
		return 0
	}

	priority := 5 // Base priority

	// Prefer blocks with moderate complexity (sweet spot for readability)
	if rec.Complexity >= 3 && rec.Complexity <= 10 {
		priority += 2
	}

	// Prefer blocks with clear inputs/outputs
	if len(rec.ReadVars) > 0 && len(rec.WriteVars) > 0 {
		priority += 1
	}

	// Prefer shorter blocks (easier to test and maintain)
	if rec.StatementCount <= 20 {
		priority += 1
	}

	// Penalize very simple blocks
	if rec.Complexity < 2 {
		priority -= 3
	}

	// Cap at 10
	if priority > 10 {
		priority = 10
	}
	if priority < 1 {
		priority = 1
	}

	return priority
}

// suggestMethodName suggests a method name based on the block characteristics
func suggestMethodName(rec ExtractionRecommendation) string {
	// Use write variables as hints
	if len(rec.WriteVars) > 0 {
		varName := rec.WriteVars[0]
		// Capitalize and add action
		return fmt.Sprintf("calculate%s", strings.ToUpper(varName[:1])+varName[1:])
	}

	// Use read variables as hints
	if len(rec.ReadVars) > 0 {
		varName := rec.ReadVars[0]
		return fmt.Sprintf("validate%s", strings.ToUpper(varName[:1])+varName[1:])
	}

	// Default names based on complexity
	if rec.Complexity > 7 {
		return "refactor"
	}
	return "extract"
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `GoRefactor Skill - Intelligent code refactoring

Usage: skill <command> [options]

Commands:
  analyze <file>                    Analyze file and recommend extractions
  refactor <file> [max]             Apply safe refactorings (default: 3)
  extract <file> <line1> <line2> <method>  Extract specific block
  suggest <file>                    Suggest refactorings without applying

Options:
  -gorefactor string                Path to gorefactor binary (default: ./gorefactor)
  -json                            Output as JSON (default: true)

Examples:
  skill analyze path/to/file.go
  skill refactor path/to/file.go 5
  skill extract path/to/file.go 10 25 processData

`)
}
