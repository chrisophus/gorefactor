// Command skill-refactor provides a Claude Code skill interface for gorefactor operations.
// This tool makes refactoring decisions intelligently and applies them efficiently.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
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
