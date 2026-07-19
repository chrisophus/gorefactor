package doctor

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// golangciLintVersion matches the pin in the Makefile and CI workflow.
const golangciLintVersion = "v2.12.2"

// noToolBootstrapEnv disables auto-provisioning of external tools. When set,
// substrates use only PATH (plus go tool for module tools).
const noToolBootstrapEnv = "GOREFACTOR_NO_TOOL_BOOTSTRAP"

// ToolBootstrapDisabled reports whether auto-provisioning is turned off.
func ToolBootstrapDisabled() bool {
	v := os.Getenv(noToolBootstrapEnv)
	return v == "1" || strings.EqualFold(v, "true")
}

// FindGolangciLint returns a golangci-lint binary on PATH or in the module-local
// cache (.gorefactor/tools/), or "" if neither exists yet.
func FindGolangciLint(root string) string {
	if p, err := exec.LookPath("golangci-lint"); err == nil {
		return p
	}
	dest := cachedGolangciLintPath(root)
	if isExecutable(dest) {
		return dest
	}
	return ""
}

// EnsureGolangciLint returns a runnable golangci-lint, bootstrapping into
// .gorefactor/tools/ on first use when allowed.
func EnsureGolangciLint(root string) (string, error) {
	if p := FindGolangciLint(root); p != "" {
		return p, nil
	}
	if ToolBootstrapDisabled() {
		return "", unavailablef("golangci-lint not on PATH (%s is set)", noToolBootstrapEnv)
	}
	dest := cachedGolangciLintPath(root)
	if err := bootstrapGolangciLint(root, dest); err != nil {
		return "", fmt.Errorf("bootstrap golangci-lint: %w", err)
	}
	if !isExecutable(dest) {
		return "", unavailablef("golangci-lint bootstrap completed but %s is not executable", dest)
	}
	return dest, nil
}

func probeGoModuleTool(root, tool string) error {
	cmd := exec.Command("go", "tool", "-n", tool)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return unavailablef("%s unavailable via go tool (declare it in go.mod tool block): %s", tool, msg)
	}
	return nil
}

func runGoModuleTool(root, tool string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"tool", tool}, args...)
	cmd := exec.Command("go", cmdArgs...)
	cmd.Dir = root
	return commandOutput(cmd, tool)
}

func runModuleOrPathTool(root, binName, toolName string, args ...string) ([]byte, error) {
	if _, err := exec.LookPath(binName); err == nil {
		return runSubstrateBinary(root, binName, args...)
	}
	if err := probeGoModuleTool(root, toolName); err != nil {
		return nil, err
	}
	return runGoModuleTool(root, toolName, args...)
}

func probeModuleOrPathTool(root, binName, toolName string) error {
	if _, err := exec.LookPath(binName); err == nil {
		return nil
	}
	return probeGoModuleTool(root, toolName)
}

func runSubstrateBinary(root, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = root
	return commandOutput(cmd, name)
}

func commandOutput(cmd *exec.Cmd, name string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	if runErr != nil && len(bytes.TrimSpace(out)) == 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		return nil, unavailablef("%s failed to run: %s", name, msg)
	}
	return out, runErr
}

func cachedGolangciLintPath(root string) string {
	name := "golangci-lint"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(root, ".gorefactor", "tools", name)
}

func bootstrapGolangciLint(root, dest string) error {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return unavailablef("create tools dir: %v", err)
	}
	installURL := golangciInstallScriptURL()
	// #nosec G107 -- URL is derived only from the pinned golangciLintVersion constant.
	resp, err := http.Get(installURL)
	if err != nil {
		return unavailablef("download golangci-lint install script: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return unavailablef("download golangci-lint install script: HTTP %s", resp.Status)
	}
	script, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return unavailablef("read golangci-lint install script: %v", err)
	}
	cmd := exec.Command("sh", "-s", "--", "-b", dir, golangciLintVersion)
	cmd.Dir = root
	cmd.Stdin = bytes.NewReader(script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return unavailablef("bootstrap golangci-lint: %s", msg)
	}
	return nil
}

func golangciInstallScriptURL() string {
	return fmt.Sprintf("https://raw.githubusercontent.com/golangci/golangci-lint/%s/install.sh", golangciLintVersion)
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}
