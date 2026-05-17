package orchestrator

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// TestRunner executes tests and reports results
type TestRunner struct {
	workDir string
}

// TestResult represents test execution results
type TestResult struct {
	Success      bool
	Output       string
	ErrorOutput  string
	ExitCode     int
	TestsPassed  int
	TestsFailed  int
	Duration     string
	PackageTests map[string]PackageTestResult
}

// PackageTestResult represents test results for a package
type PackageTestResult struct {
	Package  string
	Passed   bool
	Tests    int
	Failures int
	Duration string
}

// NewTestRunner creates a new test runner
func NewTestRunner(workDir string) *TestRunner {
	if workDir == "" {
		workDir = "."
	}
	return &TestRunner{workDir: workDir}
}

// RunTests executes go test in the working directory
func (tr *TestRunner) RunTests() *TestResult {
	result := &TestResult{
		PackageTests: make(map[string]PackageTestResult),
	}

	cmd := exec.Command("go", "test", "./...", "-v", "-race")
	cmd.Dir = tr.workDir

	output, err := cmd.CombinedOutput()
	result.Output = string(output)

	if err != nil {
		result.Success = false
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
		}
		result.ErrorOutput = err.Error()
	} else {
		result.Success = true
		result.ExitCode = 0
	}

	// Parse test output to extract counts
	tr.parseTestOutput(result)

	return result
}

// RunTestsForPackage runs tests for a specific package
func (tr *TestRunner) RunTestsForPackage(pkgPath string) *TestResult {
	result := &TestResult{
		PackageTests: make(map[string]PackageTestResult),
	}

	cmd := exec.Command("go", "test", "./"+pkgPath, "-v")
	cmd.Dir = tr.workDir

	output, err := cmd.CombinedOutput()
	result.Output = string(output)

	if err != nil {
		result.Success = false
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
		}
	} else {
		result.Success = true
		result.ExitCode = 0
	}

	tr.parseTestOutput(result)

	return result
}

// parseTestOutput extracts test metrics from test output
func (tr *TestRunner) parseTestOutput(result *TestResult) {
	lines := strings.Split(result.Output, "\n")

	for _, line := range lines {
		// Look for test result lines
		if strings.Contains(line, "PASS") {
			result.TestsPassed++
		} else if strings.Contains(line, "FAIL") {
			result.TestsFailed++
		}

		// Parse package-level results
		if strings.Contains(line, "ok") || strings.Contains(line, "FAIL") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				pkgName := parts[1]
				pkgResult := PackageTestResult{
					Package: pkgName,
					Passed:  strings.Contains(line, "ok"),
				}
				result.PackageTests[pkgName] = pkgResult
			}
		}
	}
}

// CanTestAll checks if tests can be run for the working directory
func (tr *TestRunner) CanTestAll() bool {
	// Check if working directory has any Go test files
	entries, err := os.ReadDir(tr.workDir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") && entry.Name() != "vendor" {
			// Check for test files in subdirectory
			subDir := tr.workDir + "/" + entry.Name()
			subEntries, err := os.ReadDir(subDir)
			if err == nil {
				for _, subEntry := range subEntries {
					if strings.HasSuffix(subEntry.Name(), "_test.go") {
						return true
					}
				}
			}
		} else if strings.HasSuffix(entry.Name(), "_test.go") {
			return true
		}
	}

	return false
}

// Summary returns a string summary of test results
func (r *TestResult) Summary() string {
	var sb strings.Builder
	sb.WriteString("=== Test Results ===\n")
	sb.WriteString(fmt.Sprintf("Status: %v\n", map[bool]string{true: "PASSED", false: "FAILED"}[r.Success]))
	sb.WriteString(fmt.Sprintf("Exit Code: %d\n", r.ExitCode))
	sb.WriteString(fmt.Sprintf("Tests Passed: %d\n", r.TestsPassed))
	sb.WriteString(fmt.Sprintf("Tests Failed: %d\n", r.TestsFailed))

	if len(r.PackageTests) > 0 {
		sb.WriteString("\nPackage Results:\n")
		for pkg, result := range r.PackageTests {
			status := "✓"
			if !result.Passed {
				status = "✗"
			}
			sb.WriteString(fmt.Sprintf("  %s %s\n", status, pkg))
		}
	}

	return sb.String()
}

// OutputLines returns test output as individual lines
func (r *TestResult) OutputLines() []string {
	return strings.Split(r.Output, "\n")
}
