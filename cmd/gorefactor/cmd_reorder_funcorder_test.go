package main

import (
	"strings"
	"testing"
)

const funcorderSrc = `package main

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

func TestReorderFuncorderFixesOrdering(t *testing.T) {
	writeModule(t, map[string]string{"main.go": funcorderSrc})
	captureStdout(t, func() {
		if err := reorderFuncorderCommand([]string{"main.go"}); err != nil {
			t.Fatalf("reorder-funcorder: %v", err)
		}
	})
	src := readFile(t, "main.go")

	structIdx := strings.Index(src, "type Widget struct")
	ctorIdx := strings.Index(src, "func NewWidget")
	exportedIdx := strings.Index(src, "func (w *Widget) Exported")
	unexpIdx := strings.Index(src, "func (w *Widget) unexported")
	mainIdx := strings.Index(src, "func main()")

	if structIdx < 0 || ctorIdx < 0 || exportedIdx < 0 || unexpIdx < 0 || mainIdx < 0 {
		t.Fatalf("expected all decls present in output:\n%s", src)
	}
	if !(structIdx < ctorIdx && ctorIdx < exportedIdx && exportedIdx < unexpIdx) {
		t.Errorf("expected order struct < ctor < exported < unexported, got:\n%s", src)
	}
	// main() is unrelated to the struct group and should stay after it.
	if mainIdx < unexpIdx {
		t.Errorf("unrelated decl main() should remain after the struct group:\n%s", src)
	}
}

func TestReorderFuncorderNoopWhenAlreadyOrdered(t *testing.T) {
	ordered := `package main

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
	writeModule(t, map[string]string{"main.go": ordered})
	out := captureStdout(t, func() {
		if err := reorderFuncorderCommand([]string{"main.go"}); err != nil {
			t.Fatalf("reorder-funcorder: %v", err)
		}
	})
	if !strings.Contains(out, "nothing to fix") {
		t.Errorf("already-ordered file should report nothing to fix, got: %s", out)
	}
	if readFile(t, "main.go") != ordered {
		t.Error("already-ordered file must not change")
	}
}

const funcorderLooseFuncCLISrc = `package main

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

func TestReorderFuncorderFixesLooseFunctionOrdering(t *testing.T) {
	writeModule(t, map[string]string{"main.go": funcorderLooseFuncCLISrc})
	captureStdout(t, func() {
		if err := reorderFuncorderCommand([]string{"main.go"}); err != nil {
			t.Fatalf("reorder-funcorder: %v", err)
		}
	})
	src := readFile(t, "main.go")

	initIdx := strings.Index(src, "func init()")
	helperIdx := strings.Index(src, "func helper()")
	exportedIdx := strings.Index(src, "func Exported()")
	mainIdx := strings.Index(src, "func main()")
	if initIdx < 0 || helperIdx < 0 || exportedIdx < 0 || mainIdx < 0 {
		t.Fatalf("expected all decls present in output:\n%s", src)
	}
	if !(initIdx < exportedIdx && initIdx < helperIdx) {
		t.Errorf("init() must stay in place (first decl):\n%s", src)
	}
	if !(exportedIdx < helperIdx) {
		t.Errorf("expected Exported() before helper(), got:\n%s", src)
	}
}

func TestReorderFuncorderIdempotent(t *testing.T) {
	writeModule(t, map[string]string{"main.go": funcorderSrc})
	captureStdout(t, func() {
		if err := reorderFuncorderCommand([]string{"main.go"}); err != nil {
			t.Fatalf("first run: %v", err)
		}
	})
	first := readFile(t, "main.go")
	out := captureStdout(t, func() {
		if err := reorderFuncorderCommand([]string{"main.go"}); err != nil {
			t.Fatalf("second run: %v", err)
		}
	})
	if readFile(t, "main.go") != first {
		t.Error("second run must not change the file")
	}
	if !strings.Contains(out, "nothing to fix") {
		t.Errorf("second run should report nothing to fix, got: %s", out)
	}
}
