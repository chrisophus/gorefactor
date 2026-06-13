package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeModule writes a go.mod plus the given files into a fresh temp dir,
// chdirs into it, and returns the dir.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module sigmod\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const sigGreetSrc = `package main

func Greet(name string, loud bool) string {
	if loud {
		return "HELLO " + name
	}
	return "hello " + name
}

func main() {
	_ = Greet("bob", false)
}
`

const sigGreetTestSrc = `package main

import "testing"

func TestGreet(t *testing.T) {
	if Greet("x", true) == "" {
		t.Fatal("empty")
	}
}
`

func TestChangeSignatureAddParamUpdatesAllCallSites(t *testing.T) {
	writeModule(t, map[string]string{
		"main.go":      sigGreetSrc,
		"main_test.go": sigGreetTestSrc,
	})
	if err := changeSignatureCommand([]string{"main.go", "Greet", "--add-param", "count int"}); err != nil {
		t.Fatalf("add-param: %v", err)
	}
	src := readFile(t, "main.go")
	if !strings.Contains(src, "func Greet(name string, loud bool, count int) string") {
		t.Fatalf("signature not updated:\n%s", src)
	}
	if !strings.Contains(src, `Greet("bob", false, 0)`) {
		t.Fatalf("call site not updated:\n%s", src)
	}
	testSrc := readFile(t, "main_test.go")
	if !strings.Contains(testSrc, `Greet("x", true, 0)`) {
		t.Fatalf("_test.go call site not updated:\n%s", testSrc)
	}
}

func TestChangeSignatureAddContextParam(t *testing.T) {
	writeModule(t, map[string]string{"main.go": sigGreetSrc})
	err := changeSignatureCommand([]string{"main.go", "Greet", "--add-param", "ctx context.Context", "--position", "0"})
	if err != nil {
		t.Fatalf("add ctx: %v", err)
	}
	src := readFile(t, "main.go")
	if !strings.Contains(src, "func Greet(ctx context.Context, name string, loud bool) string") {
		t.Fatalf("ctx param not first:\n%s", src)
	}
	if !strings.Contains(src, `Greet(context.TODO(), "bob", false)`) {
		t.Fatalf("call site should default to context.TODO():\n%s", src)
	}
	if !strings.Contains(src, `"context"`) {
		t.Fatalf("context import should be added:\n%s", src)
	}
}

func TestChangeSignatureAddParamCallValue(t *testing.T) {
	writeModule(t, map[string]string{"main.go": sigGreetSrc})
	err := changeSignatureCommand([]string{"main.go", "Greet", "--add-param", "tag string", "--call-value", `"v1"`})
	if err != nil {
		t.Fatalf("add-param --call-value: %v", err)
	}
	if src := readFile(t, "main.go"); !strings.Contains(src, `Greet("bob", false, "v1")`) {
		t.Fatalf("call site should use --call-value:\n%s", src)
	}
}

func TestChangeSignatureRemoveParam(t *testing.T) {
	writeModule(t, map[string]string{
		"main.go": `package main

func Run(name string, unused int) string {
	return name
}

func main() {
	_ = Run("a", 7)
}
`,
		"main_test.go": `package main

import "testing"

func TestRun(t *testing.T) {
	if Run("b", 9) == "" {
		t.Fatal("empty")
	}
}
`,
	})
	if err := changeSignatureCommand([]string{"main.go", "Run", "--remove-param", "unused"}); err != nil {
		t.Fatalf("remove-param: %v", err)
	}
	src := readFile(t, "main.go")
	if !strings.Contains(src, "func Run(name string) string") {
		t.Fatalf("param not removed:\n%s", src)
	}
	if !strings.Contains(src, `Run("a")`) {
		t.Fatalf("call site argument not dropped:\n%s", src)
	}
	if testSrc := readFile(t, "main_test.go"); !strings.Contains(testSrc, `Run("b")`) {
		t.Fatalf("_test.go argument not dropped:\n%s", testSrc)
	}
}

func TestChangeSignatureRemoveParamUsedInBodyRefuses(t *testing.T) {
	writeModule(t, map[string]string{"main.go": sigGreetSrc})
	before := readFile(t, "main.go")
	err := changeSignatureCommand([]string{"main.go", "Greet", "--remove-param", "loud"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), `"loud" is used in the body`) {
		t.Fatalf("error should explain the body use: %v", err)
	}
	if !strings.Contains(err.Error(), "main.go:") {
		t.Fatalf("error should list use locations: %v", err)
	}
	if readFile(t, "main.go") != before {
		t.Fatal("refused remove must not modify the file")
	}
}

func TestChangeSignatureRenameParam(t *testing.T) {
	writeModule(t, map[string]string{"main.go": sigGreetSrc})
	if err := changeSignatureCommand([]string{"main.go", "Greet", "--rename-param", "name", "who"}); err != nil {
		t.Fatalf("rename-param: %v", err)
	}
	src := readFile(t, "main.go")
	if !strings.Contains(src, "func Greet(who string, loud bool) string") {
		t.Fatalf("signature not renamed:\n%s", src)
	}
	if !strings.Contains(src, `"HELLO " + who`) || strings.Contains(src, `"hello " + name`) {
		t.Fatalf("body uses not renamed:\n%s", src)
	}
	// Call sites must be untouched by a rename.
	if !strings.Contains(src, `Greet("bob", false)`) {
		t.Fatalf("call site should be unchanged:\n%s", src)
	}
}

func TestChangeSignatureFuncUsedAsValueRefuses(t *testing.T) {
	writeModule(t, map[string]string{
		"main.go": sigGreetSrc,
		"ref.go":  "package main\n\nvar fn = Greet\n",
	})
	before := readFile(t, "main.go")
	err := changeSignatureCommand([]string{"main.go", "Greet", "--add-param", "n int"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), "ref.go:3") || !strings.Contains(err.Error(), "used as a value") {
		t.Fatalf("error should list the func-value site: %v", err)
	}
	if readFile(t, "main.go") != before {
		t.Fatal("refused change must not modify any file")
	}
}

func TestChangeSignatureInterfaceSatisfactionRefuses(t *testing.T) {
	writeModule(t, map[string]string{
		"svc.go": `package main

type Store interface {
	Get(key string) string
}

type MemStore struct{}

func (m *MemStore) Get(key string) string { return key }

func main() {}
`,
	})
	err := changeSignatureCommand([]string{"svc.go", "MemStore:Get", "--add-param", "n int"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), "Store") || !strings.Contains(err.Error(), "interface") {
		t.Fatalf("error should name the satisfied interface: %v", err)
	}
}

func TestChangeSignatureMethodCallSites(t *testing.T) {
	writeModule(t, map[string]string{
		"svc.go": `package main

type Svc struct{}

func (s *Svc) Ping(host string) string { return host }

func main() {
	s := &Svc{}
	_ = s.Ping("a")
}
`,
	})
	if err := changeSignatureCommand([]string{"svc.go", "Svc:Ping", "--add-param", "retries int"}); err != nil {
		t.Fatalf("method add-param: %v", err)
	}
	src := readFile(t, "svc.go")
	if !strings.Contains(src, "func (s *Svc) Ping(host string, retries int) string") {
		t.Fatalf("method signature not updated:\n%s", src)
	}
	if !strings.Contains(src, `s.Ping("a", 0)`) {
		t.Fatalf("method call site not updated:\n%s", src)
	}
}

func TestChangeSignatureOutOfScopeActions(t *testing.T) {
	writeModule(t, map[string]string{"main.go": sigGreetSrc})
	for _, args := range [][]string{
		{"main.go", "Greet", "--reorder-params", "1,0"},
		{"main.go", "Greet", "--change-returns", "error"},
	} {
		err := changeSignatureCommand(args)
		assertExitCode(t, err, exitUsage)
		if !strings.Contains(err.Error(), "not supported") {
			t.Fatalf("expected 'not supported', got: %v", err)
		}
	}
}

func TestChangeSignatureMissingTargetListsCandidates(t *testing.T) {
	writeModule(t, map[string]string{"main.go": sigGreetSrc})
	err := changeSignatureCommand([]string{"main.go", "Gret", "--add-param", "n int"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), "Greet") {
		t.Fatalf("expected candidate list with did-you-mean, got: %v", err)
	}
}

func TestChangeSignatureMissingParamListsCandidates(t *testing.T) {
	writeModule(t, map[string]string{"main.go": sigGreetSrc})
	err := changeSignatureCommand([]string{"main.go", "Greet", "--rename-param", "nme=who"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), "name") || !strings.Contains(err.Error(), "loud") {
		t.Fatalf("expected param candidates, got: %v", err)
	}
}

func TestChangeSignatureDryRunDoesNotWrite(t *testing.T) {
	writeModule(t, map[string]string{"main.go": sigGreetSrc})
	before := readFile(t, "main.go")
	out := captureStdout(t, func() {
		if err := changeSignatureCommand([]string{"main.go", "Greet", "--add-param", "n int", "--dry-run"}); err != nil {
			t.Errorf("dry-run: %v", err)
		}
	})
	if readFile(t, "main.go") != before {
		t.Fatal("--dry-run must not modify the file")
	}
	if !strings.Contains(out, "+func Greet(name string, loud bool, n int) string") {
		t.Fatalf("dry-run should print the diff, got:\n%s", out)
	}
}
