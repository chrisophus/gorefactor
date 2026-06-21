package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Scenario1MoveTargetNotFound tests moving a nonexistent function
func Scenario1MoveTargetNotFound() TestScenario {
	return TestScenario{
		Name:        "Move Command - Target Not Found",
		Description: "LLM attempts to move nonexistent function; retries with correct name",
		Setup: func(testDir string) error {
			content := `package main

func ProcessRequest() string {
    return "handled"
}
`
			_, err := WriteGoFile(testDir, "handlers.go", content)
			return err
		},
		InitialCommand: []string{
			"gorefactor", "move", "handlers.go", "NonExistent", "other.go", "--json",
		},
		ExpectedErrorCode: "FUNCTION_NOT_FOUND",
		RecoverySteps: []RecoveryStep{
			{
				Description: "Retry move with correct function name",
				Command:     []string{"gorefactor", "move", "handlers.go", "ProcessRequest", "other.go", "--json"},
				ExpectError: false,
			},
		},
		Validate: func(testDir string) error {
			// Check that other.go was created and contains ProcessRequest
			if !FileExists(filepath.Join(testDir, "other.go")) {
				return fmt.Errorf("other.go not created")
			}
			if !FileContains(filepath.Join(testDir, "other.go"), "func ProcessRequest") {
				return fmt.Errorf("ProcessRequest not in other.go")
			}
			return nil
		},
	}
}

// Scenario2ExtractReturnStatement tests extracting code with return statements
func Scenario2ExtractReturnStatement() TestScenario {
	return TestScenario{
		Name:        "Extract Command - Return Statement",
		Description: "LLM attempts to extract block with returns; creates refactored version",
		Setup: func(testDir string) error {
			content := `package main

func Process(data string) (bool, error) {
    if data == "" {
        return false, nil
    }
    result := len(data) > 0
    return result, nil
}
`
			_, err := WriteGoFile(testDir, "processor.go", content)
			return err
		},
		InitialCommand: []string{
			"gorefactor", "extract", "processor.go", "4", "8", "validate", "--json",
		},
		ExpectedErrorCode: "RETURN_IN_BLOCK",
		RecoverySteps: []RecoveryStep{
			{
				Description: "Extract just the length check without returns",
				Command:     []string{"gorefactor", "extract", "processor.go", "7", "7", "checkLength", "--json"},
				ExpectError: false,
			},
		},
		Validate: func(testDir string) error {
			if !FileContains(filepath.Join(testDir, "processor.go"), "func checkLength") {
				return fmt.Errorf("extracted function checkLength not created")
			}
			return nil
		},
	}
}

// Scenario3DeleteHasCallers tests deleting a function with callers
func Scenario3DeleteHasCallers() TestScenario {
	return TestScenario{
		Name:        "Delete Command - Has Callers",
		Description: "LLM attempts to delete function with callers; updates caller then retries delete",
		Setup: func(testDir string) error {
			service := `package main

func Helper() string {
    return "help"
}
`
			if _, err := WriteGoFile(testDir, "service.go", service); err != nil {
				return err
			}

			main := `package main

import "fmt"

func main() {
    fmt.Println(Helper())
}
`
			_, err := WriteGoFile(testDir, "main.go", main)
			return err
		},
		InitialCommand: []string{
			"gorefactor", "delete", "service.go", "Helper", "--safe", "--json",
		},
		ExpectedErrorCode: "HAS_CALLERS",
		RecoverySteps: []RecoveryStep{
			{
				Description: "Replace call to Helper with literal",
				Command:     []string{"gorefactor", "replace-text", "main.go", "main", "Helper()", "\"help\"", "--json"},
				ExpectError: false,
			},
			{
				Description: "Retry delete without --safe",
				Command:     []string{"gorefactor", "delete", "service.go", "Helper", "--json"},
				ExpectError: false,
			},
		},
		Validate: func(testDir string) error {
			if FileContains(filepath.Join(testDir, "service.go"), "func Helper") {
				return fmt.Errorf("Helper should be deleted")
			}
			if !FileContains(filepath.Join(testDir, "main.go"), "\"help\"") {
				return fmt.Errorf("main.go should be updated")
			}
			return nil
		},
	}
}

// Scenario4ReplacePatternNotFound tests replacing a pattern that doesn't exist
func Scenario4ReplacePatternNotFound() TestScenario {
	return TestScenario{
		Name:        "Replace Command - Pattern Not Found",
		Description: "LLM attempts to replace pattern with wrong spacing, gets error; retries with correct spacing",
		Setup: func(testDir string) error {
			content := `package main

func Add(a, b int) int {
    result := a + b
    return result
}
`
			_, err := WriteGoFile(testDir, "math.go", content)
			return err
		},
		InitialCommand: []string{
			"gorefactor", "replace-text", "math.go", "Add", "a+b", "a*b", "--json",
		},
		ExpectedErrorCode: "PATTERN_NOT_FOUND",
		RecoverySteps: []RecoveryStep{
			{
				Description: "Retry with correct spacing (with spaces)",
				Command:     []string{"gorefactor", "replace-text", "math.go", "Add", "a + b", "a * b", "--json"},
				ExpectError: false,
			},
		},
		Validate: func(testDir string) error {
			if !FileContains(filepath.Join(testDir, "math.go"), "a * b") {
				return fmt.Errorf("pattern not replaced")
			}
			return nil
		},
	}
}

// AllScenarios returns all test scenarios
func AllScenarios() []TestScenario {
	return []TestScenario{
		Scenario1MoveTargetNotFound(),
		Scenario2ExtractReturnStatement(),
		Scenario3DeleteHasCallers(),
		Scenario4ReplacePatternNotFound(),
	}
}

// GetScenarioByName returns a scenario by name or nil if not found
func GetScenarioByName(name string) *TestScenario {
	for _, s := range AllScenarios() {
		if strings.EqualFold(s.Name, name) {
			scenario := s
			return &scenario
		}
	}
	return nil
}

// PrintScenarios lists all available scenarios
func PrintScenarios() {
	fmt.Println("\nAvailable Scenarios:")
	fmt.Println("====================")
	for i, scenario := range AllScenarios() {
		fmt.Printf("\n%d. %s\n", i+1, scenario.Name)
		fmt.Printf("   %s\n", scenario.Description)
		fmt.Printf("   Expected Error: %s\n", scenario.ExpectedErrorCode)
		fmt.Printf("   Recovery Steps: %d\n", len(scenario.RecoverySteps))
	}
	fmt.Println()
}
