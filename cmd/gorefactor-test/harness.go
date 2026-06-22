package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TestScenario represents a single test case for error recovery
type TestScenario struct {
	Name              string
	Description       string
	Setup             func(testDir string) error
	InitialCommand    []string
	ExpectedErrorCode string
	RecoverySteps     []RecoveryStep
	Validate          func(testDir string) error
}

// RecoveryStep is a single action to recover from an error
type RecoveryStep struct {
	Description string
	Command     []string
	ExpectError bool
}

// TestResult tracks metrics for a single scenario
type TestResult struct {
	ScenarioName    string
	Success         bool
	InitialError    string
	ErrorCode       string
	RecoveryAttempt int
	TotalSteps      int
	ErrorMessage    string
	Suggestions     []string
}

// RunScenario executes a test scenario and collects results
func RunScenario(scenario TestScenario, testDir string) TestResult {
	result := TestResult{
		ScenarioName: scenario.Name,
		TotalSteps:   len(scenario.RecoverySteps) + 1,
	}

	// Step 0: Setup
	if scenario.Setup != nil {
		if err := scenario.Setup(testDir); err != nil {
			result.ErrorMessage = fmt.Sprintf("Setup failed: %v", err)
			return result
		}
	}

	// Step 1: Initial command (expect error)
	initialErr := executeCommand(testDir, scenario.InitialCommand)
	if initialErr == nil {
		result.ErrorMessage = "Expected error but command succeeded"
		return result
	}
	result.InitialError = initialErr.Error()

	// Extract error details if JSON
	if hasJSONFlag(scenario.InitialCommand) {
		errCode, suggestions := parseJSONError(initialErr.Error())
		result.ErrorCode = errCode
		result.Suggestions = suggestions
	}

	// Steps 2+: Recovery steps
	for i, step := range scenario.RecoverySteps {
		err := executeCommand(testDir, step.Command)
		// Check if outcome matches expectation
		if step.ExpectError && err == nil {
			result.ErrorMessage = fmt.Sprintf("Step %d (%s): expected error but command succeeded", i+2, step.Description)
			return result
		}
		if !step.ExpectError && err != nil {
			result.ErrorMessage = fmt.Sprintf("Step %d (%s): %v", i+2, step.Description, err)
			return result
		}
		result.RecoveryAttempt = i + 1
	}

	// Final validation
	if scenario.Validate != nil {
		if err := scenario.Validate(testDir); err != nil {
			result.ErrorMessage = fmt.Sprintf("Validation failed: %v", err)
			return result
		}
	}

	result.Success = true
	return result
}

// executeCommand runs a command and returns error if it fails
func executeCommand(testDir string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("empty command")
	}

	// If first arg is "gorefactor", use absolute path
	cmdPath := args[0]
	if args[0] == "gorefactor" {
		if path, err := exec.LookPath("gorefactor"); err == nil {
			cmdPath = path
		}
	}

	cmd := exec.Command(cmdPath, args[1:]...)
	cmd.Dir = testDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String() + stderr.String()

	// Command succeeded
	if err == nil {
		return nil
	}

	// Command failed - return the output as error message
	if output != "" {
		return fmt.Errorf("%s", output)
	}

	return err
}

// hasJSONFlag checks if command includes --json
func hasJSONFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}

// parseJSONError extracts error code and suggestions from JSON output
func parseJSONError(output string) (string, []string) {
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		return "", nil
	}

	// Try to get errorDetails first (new format)
	if errDetails, ok := parsed["errorDetails"].(map[string]interface{}); ok {
		if code, ok := errDetails["code"].(string); ok {
			var suggestions []string
			if suggs, ok := errDetails["suggestions"].([]interface{}); ok {
				for _, s := range suggs {
					if sMap, ok := s.(map[string]interface{}); ok {
						if approach, ok := sMap["approach"].(string); ok {
							suggestions = append(suggestions, approach)
						}
					}
				}
			}
			return code, suggestions
		}
	}

	// Fallback: parse error if present
	if _, ok := parsed["error"].(string); ok {
		return "PARSE_ERROR", []string{}
	}

	return "", nil
}

// PrintResult formats a test result for display
func PrintResult(result TestResult) {
	status := "❌"
	if result.Success {
		status = "✅"
	}

	fmt.Printf("%s %s\n", status, result.ScenarioName)
	if result.ErrorCode != "" {
		fmt.Printf("   Error Code: %s\n", result.ErrorCode)
	}
	if len(result.Suggestions) > 0 {
		fmt.Printf("   Suggestions: %s\n", strings.Join(result.Suggestions, ", "))
	}
	if result.RecoveryAttempt > 0 {
		fmt.Printf("   Recovery Attempts: %d/%d\n", result.RecoveryAttempt, len(result.Suggestions))
	}
	if result.ErrorMessage != "" {
		fmt.Printf("   Error: %s\n", result.ErrorMessage)
	}
}

// CreateTestDir creates a temporary directory for tests
func CreateTestDir() (string, error) {
	dir, err := os.MkdirTemp("", "gorefactor-test-*")
	if err != nil {
		return "", err
	}

	// Initialize go.mod
	modPath := filepath.Join(dir, "go.mod")
	content := "module test\n\ngo 1.24\n"
	if err := os.WriteFile(modPath, []byte(content), 0644); err != nil {
		return "", err
	}

	return dir, nil
}

// WriteGoFile writes a Go file to the test directory
func WriteGoFile(testDir, filename, content string) (string, error) {
	path := filepath.Join(testDir, filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// FileExists checks if a file exists
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// FileContains checks if a file contains a substring
func FileContains(path, substr string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), substr)
}
