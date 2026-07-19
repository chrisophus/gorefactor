package orchestrator

import (
	"os/exec"
	"strings"
)

// TestRunner executes tests and reports results
type TestRunner struct {
	workDir string
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

// Check if working directory has any Go test files

// Check for test files in subdirectory

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
