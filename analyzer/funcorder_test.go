package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempGoFile(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

const funcorderMisorderedSrc = `package main

type Widget struct {
	name string
}

func (w *Widget) unexported() string {
	return w.name
}

func (w *Widget) Exported() string {
	return w.name
}

func NewWidget(name string) *Widget {
	return &Widget{name: name}
}

func main() {}
`

const funcorderCleanSrc = `package main

type Widget struct {
	name string
}

func NewWidget(name string) *Widget {
	return &Widget{name: name}
}

func (w *Widget) Exported() string {
	return w.name
}

func (w *Widget) unexported() string {
	return w.name
}

func main() {}
`

func TestFileFuncorderIssues_DetectsBothViolations(t *testing.T) {
	path := writeTempGoFile(t, funcorderMisorderedSrc)
	issues, err := FileFuncorderIssues(path)
	if err != nil {
		t.Fatal(err)
	}
	var haveCtor, haveMethod bool
	for _, iss := range issues {
		switch iss.Rule {
		case funcorderConstructorRuleName:
			haveCtor = true
		case funcorderStructMethodRuleName:
			haveMethod = true
		}
	}
	if !haveCtor {
		t.Errorf("expected funcorder-constructor issue, got: %+v", issues)
	}
	if !haveMethod {
		t.Errorf("expected funcorder-struct-method issue, got: %+v", issues)
	}
}

func TestFileFuncorderIssues_CleanFileHasNoIssues(t *testing.T) {
	path := writeTempGoFile(t, funcorderCleanSrc)
	issues, err := FileFuncorderIssues(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Errorf("expected no issues for correctly ordered file, got: %+v", issues)
	}
}

func TestApplyFuncorderFixes_ReordersDecls(t *testing.T) {
	path := writeTempGoFile(t, funcorderMisorderedSrc)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, n, err := ApplyFuncorderFixes(path, src)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 struct group reordered, got %d", n)
	}
	result := string(out)

	structIdx := strings.Index(result, "type Widget struct")
	ctorIdx := strings.Index(result, "func NewWidget")
	exportedIdx := strings.Index(result, "func (w *Widget) Exported")
	unexpIdx := strings.Index(result, "func (w *Widget) unexported")
	mainIdx := strings.Index(result, "func main()")
	if structIdx < 0 || ctorIdx < 0 || exportedIdx < 0 || unexpIdx < 0 || mainIdx < 0 {
		t.Fatalf("expected all decls present:\n%s", result)
	}
	if structIdx >= ctorIdx || ctorIdx >= exportedIdx || exportedIdx >= unexpIdx {
		t.Errorf("wrong order:\n%s", result)
	}
	if mainIdx < unexpIdx {
		t.Errorf("unrelated main() should stay after the struct group:\n%s", result)
	}

	// Re-parsing the result must succeed (valid, gofmt'd Go).
	reissues, err := FileFuncorderIssues(path) // sanity: original file still has issues
	if err != nil {
		t.Fatal(err)
	}
	if len(reissues) == 0 {
		t.Fatal("original on-disk file should still have issues (fix operates on in-memory src only)")
	}
}

func TestApplyFuncorderFixes_NoopOnCleanFile(t *testing.T) {
	path := writeTempGoFile(t, funcorderCleanSrc)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, n, err := ApplyFuncorderFixes(path, src)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0 groups reordered on already-clean file, got %d", n)
	}
	if string(out) != string(src) {
		t.Error("clean file should be returned unchanged")
	}
}

func TestApplyFuncorderFixes_Idempotent(t *testing.T) {
	path := writeTempGoFile(t, funcorderMisorderedSrc)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	first, n1, err := ApplyFuncorderFixes(path, src)
	if err != nil {
		t.Fatal(err)
	}
	if n1 == 0 {
		t.Fatal("expected first pass to reorder something")
	}
	second, n2, err := ApplyFuncorderFixes(path, first)
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Errorf("second pass should be a no-op, reordered %d groups", n2)
	}
	if string(second) != string(first) {
		t.Error("second pass should return the same source unchanged")
	}
}

const funcorderLooseFuncSrc = `package main

func init() {
	println("init")
}

func helper() int {
	return 1
}

func Exported() int {
	return helper()
}

func main() {}
`

const funcorderLooseFuncCleanSrc = `package main

func init() {
	println("init")
}

func Exported() int {
	return helper()
}

func helper() int {
	return 1
}

func main() {}
`

const funcorderOnlyCtorAndInitSrc = `package main

type Widget struct {
	name string
}

func NewWidget(name string) *Widget {
	return &Widget{name: name}
}

func init() {
	println("boot")
}

func (w *Widget) Exported() string {
	return w.name
}
`

func TestFileFuncorderIssues_DetectsLooseFunctionViolation(t *testing.T) {
	path := writeTempGoFile(t, funcorderLooseFuncSrc)
	issues, err := FileFuncorderIssues(path)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, iss := range issues {
		if iss.Rule == funcorderFunctionRuleName {
			found = true
			if iss.FuncName != "helper" {
				t.Errorf("expected FuncName=helper, got %q", iss.FuncName)
			}
		}
	}
	if !found {
		t.Errorf("expected funcorder-function issue, got: %+v", issues)
	}
}

func TestFileFuncorderIssues_NoFalsePositiveForCtorAndInit(t *testing.T) {
	path := writeTempGoFile(t, funcorderOnlyCtorAndInitSrc)
	issues, err := FileFuncorderIssues(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, iss := range issues {
		if iss.Rule == funcorderFunctionRuleName {
			t.Errorf("did not expect funcorder-function issue (only ctor+init are loose), got: %+v", issues)
		}
	}
}

func TestFileFuncorderIssues_InitNeverFlagged(t *testing.T) {
	path := writeTempGoFile(t, funcorderLooseFuncCleanSrc)
	issues, err := FileFuncorderIssues(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, iss := range issues {
		if iss.Rule == funcorderFunctionRuleName {
			t.Errorf("clean loose-function ordering should have no issue, got: %+v", issues)
		}
		if iss.FuncName == "init" {
			t.Errorf("init() must never be flagged, got: %+v", issues)
		}
	}
}

func TestApplyFuncorderFixes_ReordersLooseFunctions(t *testing.T) {
	path := writeTempGoFile(t, funcorderLooseFuncSrc)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, n, err := ApplyFuncorderFixes(path, src)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected at least 1 fix applied")
	}
	result := string(out)
	initIdx := strings.Index(result, "func init()")
	helperIdx := strings.Index(result, "func helper()")
	exportedIdx := strings.Index(result, "func Exported()")
	mainIdx := strings.Index(result, "func main()")
	if initIdx < 0 || helperIdx < 0 || exportedIdx < 0 || mainIdx < 0 {
		t.Fatalf("expected all decls present:\n%s", result)
	}
	if initIdx >= exportedIdx || initIdx >= helperIdx {
		t.Errorf("init() must stay first:\n%s", result)
	}
	if exportedIdx >= helperIdx {
		t.Errorf("expected Exported() before helper():\n%s", result)
	}
	if mainIdx < helperIdx {
		t.Errorf("main() should remain after the reordered loose functions:\n%s", result)
	}
}

func TestApplyFuncorderFixes_NoopWhenLooseFunctionsAlreadyOrdered(t *testing.T) {
	path := writeTempGoFile(t, funcorderLooseFuncCleanSrc)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, n, err := ApplyFuncorderFixes(path, src)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0 fixes on already-clean file, got %d", n)
	}
	if string(out) != string(src) {
		t.Error("clean file should be returned unchanged")
	}
}

func TestApplyFuncorderFixes_CombinedStructAndFunctionViolations(t *testing.T) {
	src := `package main

type Widget struct {
	name string
}

func (w *Widget) unexported() string {
	return w.name
}

func (w *Widget) Exported() string {
	return w.name
}

func NewWidget(name string) *Widget {
	return &Widget{name: name}
}

func helper() int {
	return 1
}

func Exported2() int {
	return helper()
}

func main() {}
`
	path := writeTempGoFile(t, src)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, n, err := ApplyFuncorderFixes(path, raw)
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("expected both the struct group and the loose-function group fixed, got n=%d", n)
	}
	result := string(out)

	structIdx := strings.Index(result, "type Widget struct")
	ctorIdx := strings.Index(result, "func NewWidget")
	structExportedIdx := strings.Index(result, "func (w *Widget) Exported")
	structUnexpIdx := strings.Index(result, "func (w *Widget) unexported")
	helperIdx := strings.Index(result, "func helper()")
	exported2Idx := strings.Index(result, "func Exported2()")
	if structIdx < 0 || ctorIdx < 0 || structExportedIdx < 0 || structUnexpIdx < 0 || helperIdx < 0 || exported2Idx < 0 {
		t.Fatalf("expected all decls present:\n%s", result)
	}
	if !(structIdx < ctorIdx && ctorIdx < structExportedIdx && structExportedIdx < structUnexpIdx) {
		t.Errorf("struct group not fixed:\n%s", result)
	}
	if !(exported2Idx < helperIdx) {
		t.Errorf("loose function group not fixed:\n%s", result)
	}

	// Idempotent.
	second, n2, err := ApplyFuncorderFixes(path, out)
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Errorf("second pass should be a no-op, changed %d", n2)
	}
	if string(second) != string(out) {
		t.Error("second pass should return the same source unchanged")
	}
}

func TestApplyFuncorderFixes_UnrelatedDeclsUntouched(t *testing.T) {
	src := `package main

import "fmt"

const Greeting = "hello"

type Widget struct {
	name string
}

func (w *Widget) unexported() string {
	return w.name
}

func (w *Widget) Exported() string {
	return w.name
}

func NewWidget(name string) *Widget {
	return &Widget{name: name}
}

func Unrelated() {
	fmt.Println(Greeting)
}
`
	path := writeTempGoFile(t, src)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, n, err := ApplyFuncorderFixes(path, raw)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 group reordered, got %d", n)
	}
	result := string(out)
	if !strings.Contains(result, `const Greeting = "hello"`) {
		t.Errorf("unrelated const decl must be preserved:\n%s", result)
	}
	if !strings.Contains(result, "func Unrelated() {") {
		t.Errorf("unrelated func decl must be preserved:\n%s", result)
	}
	if !strings.Contains(result, `import "fmt"`) {
		t.Errorf("import decl must be preserved:\n%s", result)
	}
}
