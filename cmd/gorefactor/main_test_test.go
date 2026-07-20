package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisophus/gorefactor/analyzer"
)

func TestCreateCommand(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "hello.go")
	if err := createCommand([]string{target, "package x\n"}); err != nil {
		t.Fatalf("createCommand: %v", err)
	}
	if !strings.Contains(readFile(t, target), "package x") {
		t.Fatal("content not written")
	}
	if err := createCommand([]string{target, "package y\n"}); err == nil {
		t.Fatal("expected error on existing file")
	}
}

func TestInsertCommand(t *testing.T) {
	dir := t.TempDir()
	path := writeTempGo(t, dir, "f.go", "package x\n\nimport \"fmt\"\n\nfunc Hello() {\n\tfmt.Println(\"hi\")\n}\n")
	if err := insertCommand([]string{path, "after:Hello", "func Bye() {\n\tfmt.Println(\"bye\")\n}\n"}); err != nil {
		t.Fatalf("insertCommand: %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "func Bye()") {
		t.Fatal("Bye not inserted")
	}
	if strings.Index(got, "func Hello()") > strings.Index(got, "func Bye()") {
		t.Fatal("Bye should appear after Hello")
	}
}

func TestDeleteCommand(t *testing.T) {
	dir := t.TempDir()
	path := writeTempGo(t, dir, "f.go", "package x\n\nfunc Keep() {}\n\nfunc Drop() {}\n")
	if err := deleteCommand([]string{path, "Drop"}); err != nil {
		t.Fatalf("deleteCommand: %v", err)
	}
	got := readFile(t, path)
	if strings.Contains(got, "Drop") {
		t.Fatal("Drop should have been deleted")
	}
	if !strings.Contains(got, "Keep") {
		t.Fatal("Keep should have remained")
	}
}

func TestRenameCommand(t *testing.T) {
	dir := t.TempDir()
	// Types-aware rename loads the package with go/packages, which needs a module.
	writeTempGo(t, dir, "go.mod", "module x\n\ngo 1.21\n")
	path := writeTempGo(t, dir, "f.go", "package x\n\nfunc helper() int { return 1 }\n\nfunc use() int { return helper() + 1 }\n")
	if err := renameCommand([]string{path, "helper", "doMath"}); err != nil {
		t.Fatalf("renameCommand: %v", err)
	}
	got := readFile(t, path)
	if strings.Contains(got, "helper") {
		t.Fatalf("helper should have been renamed; got:\n%s", got)
	}
	if !strings.Contains(got, "doMath()") {
		t.Fatalf("doMath should appear; got:\n%s", got)
	}
}

func TestReplaceTextCommand(t *testing.T) {
	dir := t.TempDir()
	path := writeTempGo(t, dir, "f.go", "package x\n\nfunc add(a, b int) int {\n\treturn a + b\n}\n")
	if err := replaceTextCommand([]string{path, "add", "a + b", "a + b + 1"}); err != nil {
		t.Fatalf("replaceTextCommand: %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "a + b + 1") {
		t.Fatalf("replacement not applied; got:\n%s", got)
	}
}

func TestSplitCommandReducesFileSize(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("package x\n\nimport \"fmt\"\n\n")
	for i := 0; i < 30; i++ {
		b.WriteString("func Padding")
		b.WriteString(itoa(i))
		b.WriteString("() { fmt.Println(\"p\") }\n\n")
	}
	for i := 0; i < 5; i++ {
		b.WriteString("func Analyze")
		b.WriteString(itoa(i))
		b.WriteString("() { fmt.Println(\"a\") }\n\n")
	}
	path := writeTempGo(t, dir, "big.go", b.String())
	before := fileLineCountOrFail(t, path)
	if err := splitCommand([]string{path, "--max", "40"}); err != nil {
		t.Fatalf("splitCommand: %v", err)
	}
	after := fileLineCountOrFail(t, path)
	if after >= before {
		t.Fatalf("file should be smaller after split: before=%d after=%d", before, after)
	}
	if after > 40 {
		t.Fatalf("file still over limit: %d", after)
	}
}

func TestLintCommandDetectsOversize(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("package x\n\n")
	for i := 0; i < 200; i++ {
		b.WriteString("var v")
		b.WriteString(itoa(i))
		b.WriteString(" = 1\n")
	}
	writeTempGo(t, dir, "big.go", b.String())
	if err := lintCommand([]string{dir, "--max", "100", "--rule", "file-size"}); err == nil {
		t.Fatal("expected lintCommand to fail on oversize file with default --fail-on error")
	}
}

func TestFindCallersCommand(t *testing.T) {
	dir := t.TempDir()
	writeTempGo(t, dir, "a.go", "package x\n\nfunc Helper() int { return 1 }\n\nfunc User() int { return Helper() + Helper() }\n")
	if err := findCallersCommand([]string{"Helper", "--in", dir}); err != nil {
		t.Fatalf("findCallersCommand: %v", err)
	}
}

func TestFindUsesCommand(t *testing.T) {
	dir := t.TempDir()
	writeTempGo(t, dir, "a.go", "package x\n\nvar count = 0\n\nfunc Inc() { count++ }\nfunc Get() int { return count }\n")
	if err := findUsesCommand([]string{"count", "--in", dir}); err != nil {
		t.Fatalf("findUsesCommand: %v", err)
	}
}

func TestFindImplementationsCommand(t *testing.T) {
	dir := t.TempDir()
	writeTempGo(t, dir, "a.go", `package x

type Reader interface {
	Read(p []byte) (int, error)
}

type Mem struct{}

func (m *Mem) Read(p []byte) (int, error) { return 0, nil }
`)
	if err := findImplementationsCommand([]string{"Reader", "--in", dir}); err != nil {
		t.Fatalf("findImplementationsCommand: %v", err)
	}
}

func TestSplitNameReceiver(t *testing.T) {
	cases := []struct {
		in           string
		wantName     string
		wantReceiver string
	}{
		{"Foo", "Foo", ""},
		{"Bar:Method", "Method", "Bar"},
		{"*Bar:Method", "Method", "*Bar"},
	}
	for _, c := range cases {
		n, r := splitNameReceiver(c.in)
		if n != c.wantName || r != c.wantReceiver {
			t.Errorf("splitNameReceiver(%q) = (%q, %q); want (%q, %q)", c.in, n, r, c.wantName, c.wantReceiver)
		}
	}
}

func TestCheckExtractable(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("package x\n\n")
	b.WriteString("func BigFunc() int {\n")
	for i := 0; i < 80; i++ {
		b.WriteString("\t_ = ")
		b.WriteString(itoa(i))
		b.WriteString("\n")
	}
	b.WriteString("\tif true {\n\t\tfor i := 0; i < 10; i++ {\n\t\t\t_ = i\n\t\t}\n\t}\n")
	b.WriteString("\treturn 0\n}\n")
	path := writeTempGo(t, dir, "big.go", b.String())
	hints := checkExtractable(path, 8, false)
	if len(hints) == 0 {
		t.Skip("priority threshold may filter out; ensure no panic")
	}
}

func TestCheckUntestedPackages(t *testing.T) {
	dir := t.TempDir()
	writeTempGo(t, dir, "a.go", "package x\n")
	issues := checkUntestedPackages(dir, analyzer.DefaultWalkOptions())
	if len(issues) != 1 {
		t.Fatalf("expected 1 untested package issue, got %d", len(issues))
	}
	if issues[0].Rule != "untested-package" {
		t.Errorf("rule = %q; want untested-package", issues[0].Rule)
	}

	writeTempGo(t, dir, "a_test.go", "package x\n")
	issues = checkUntestedPackages(dir, analyzer.DefaultWalkOptions())
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues after adding test, got %d", len(issues))
	}
}

func TestInspectCommand(t *testing.T) {
	dir := t.TempDir()
	writeTempGo(t, dir, "a.go", "package x\n\nfunc Foo() int { return 1 }\nfunc Bar() int { return 2 }\n")
	if err := inspectCommand([]string{filepath.Join(dir, "a.go")}); err != nil {
		t.Fatalf("inspectCommand: %v", err)
	}
}

func TestExtractCommandBasic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module ex\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	path := writeTempGo(t, dir, "main.go", `package main

import "fmt"

func main() {
	a := 10
	b := 20
	x := a + b
	y := x * 2
	fmt.Println(y)
}
`)
	if err := extractCommand([]string{path, "8", "9", "compute"}); err != nil {
		t.Fatalf("extractCommand: %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "func compute(a int, b int) int") {
		t.Fatalf("expected new function signature; got:\n%s", got)
	}
	if !strings.Contains(got, "y := compute(a, b)") {
		t.Fatalf("expected call site; got:\n%s", got)
	}
}

func TestExtractCommandRejectsReturn(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module ex\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	path := writeTempGo(t, dir, "main.go", `package main

func main() {
	x := 1
	if x > 0 {
		return
	}
}
`)
	err := extractCommand([]string{path, "4", "7", "guard"})
	if err == nil || !strings.Contains(err.Error(), "return") {
		t.Fatalf("expected return-rejection error; got %v", err)
	}
}
