package extract_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/chrisophus/gorefactor/internal/cerr"
	"github.com/chrisophus/gorefactor/refactor/extract"
)

// TestPlanMethodApply drives the engine end-to-end without touching package
// main: plan an extraction, apply it, and confirm the module still builds.
func TestPlanMethodApply(t *testing.T) {
	const src = `package exmod

func Calc(items []string) int {
	total := 0
	for _, it := range items {
		total += len(it)
	}
	total *= 2
	return total
}
`
	path := writeModule(t, src)
	plan, err := extract.PlanMethod(path, 5, 7, "sumItems", false)
	if err != nil {
		t.Fatalf("PlanMethod: %v", err)
	}
	if err := plan.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = filepath.Dir(path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("module did not build after extraction: %v\n%s", err, out)
	}
}

// TestPlanMethodReturnsRefused verifies the engine returns a classified
// *ReturnsRefusedError (carrying the return line) when a return-bearing block
// is extracted without allowReturns.
func TestPlanMethodReturnsRefused(t *testing.T) {
	const src = `package exmod

import "errors"

func Process(items []string) (int, error) {
	if len(items) == 0 {
		return 0, errors.New("no items")
	}
	return len(items), nil
}
`
	path := writeModule(t, src)
	_, err := extract.PlanMethod(path, 6, 8, "check", false)
	var refused *extract.ReturnsRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("expected *ReturnsRefusedError, got %v", err)
	}
	if len(refused.ReturnLines) == 0 {
		t.Fatal("expected refused error to carry return line(s)")
	}
}

// TestPlanMethodNotAligned confirms boundary refusals are classified as exit-2
// (not-found) errors that importers can map without string matching.
func TestPlanMethodNotAligned(t *testing.T) {
	const src = `package exmod

func Calc() int {
	total := 0
	total += 1
	return total
}
`
	path := writeModule(t, src)
	_, err := extract.PlanMethod(path, 100, 101, "helper", false)
	if err == nil {
		t.Fatal("expected error for out-of-range extraction")
	}
	if code := cerr.ExitCodeFor(err); code != cerr.ExitNotFound {
		t.Fatalf("expected exit %d, got %d (%v)", cerr.ExitNotFound, code, err)
	}
}

// writeModule drops src into a throwaway module so the engine's package loader
// (which type-checks the enclosing package) has something to resolve against.
func writeModule(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module exmod\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "code.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
