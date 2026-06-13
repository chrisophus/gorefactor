package main

import (
	"strings"
	"testing"
)

func TestInlineSingleReturnExpression(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go",
		"package x\n\nfunc double(n int) int {\n\treturn n * 2\n}\n\nfunc use() int {\n\treturn double(21) + 1\n}\n")

	if err := inlineCommand([]string{path, "double"}); err != nil {
		t.Fatalf("inline: %v", err)
	}
	got := readFile(t, path)
	if strings.Contains(got, "func double") {
		t.Fatalf("function should be deleted:\n%s", got)
	}
	if !strings.Contains(got, "(21 * 2) + 1") {
		t.Fatalf("call should be replaced with the substituted expression:\n%s", got)
	}
}

func TestInlineStatementBodyAcrossFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go",
		"package x\n\nfunc note(msg string) {\n\tprintln(\"note:\", msg)\n}\n")
	other := writeTempGo(t, ".", "g.go",
		"package x\n\nfunc work() {\n\tnote(\"start\")\n\tnote(\"end\")\n}\n")

	if err := inlineCommand([]string{path, "note"}); err != nil {
		t.Fatalf("inline: %v", err)
	}
	if strings.Contains(readFile(t, path), "func note") {
		t.Fatal("function should be deleted")
	}
	got := readFile(t, other)
	if !strings.Contains(got, "println(\"note:\", \"start\")") || !strings.Contains(got, "println(\"note:\", \"end\")") {
		t.Fatalf("both call sites should be inlined:\n%s", got)
	}
}

func TestInlineExprResultDiscardedKeepsEvaluation(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go",
		"package x\n\nfunc calc(n int) int {\n\treturn step(n) + 1\n}\n\nfunc step(n int) int { return n }\n\nfunc use() {\n\tcalc(3)\n}\n")

	if err := inlineCommand([]string{path, "calc"}); err != nil {
		t.Fatalf("inline: %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "_ = step(3) + 1") {
		t.Fatalf("discarded result should become `_ =` to keep evaluation:\n%s", got)
	}
}

func TestInlineRefusals(t *testing.T) {
	t.Chdir(t.TempDir())

	cases := []struct {
		name string
		src  string
		fn   string
		want string
	}{
		{
			"multiple results",
			"package x\n\nfunc two() (int, int) {\n\treturn 1, 2\n}\n\nfunc use() { a, b := two(); _, _ = a, b }\n",
			"two", "single-value returns",
		},
		{
			"defer",
			"package x\n\nfunc d(f func()) int {\n\tdefer f()\n\treturn 1\n}\n",
			"d", "defer",
		},
		{
			"recursion",
			"package x\n\nfunc rec(n int) int {\n\treturn rec(n - 1)\n}\n",
			"rec", "recursive",
		},
		{
			"closure",
			"package x\n\nfunc c(n int) func() int {\n\treturn func() int { return n }\n}\n",
			"c", "closure",
		},
		{
			"param used twice",
			"package x\n\nfunc sq(n int) int {\n\treturn n * n\n}\n\nfunc use() int { return sq(3) }\n",
			"sq", "used 2 times",
		},
		{
			"used as value",
			"package x\n\nfunc one() int {\n\treturn 1\n}\n\nvar f = one\n",
			"one", "used as a value",
		},
		{
			"body declares variables",
			"package x\n\nfunc decl(n int) {\n\tx := n\n\tprintln(x)\n}\n\nfunc use() { decl(1) }\n",
			"decl", "declares variables",
		},
		{
			"address of parameter",
			"package x\n\nfunc addr(n int) *int {\n\treturn &n\n}\n\nfunc use() *int { return addr(1) }\n",
			"addr", "address of parameter",
		},
		{
			"variadic",
			"package x\n\nfunc v(ns ...int) int {\n\treturn len(ns)\n}\n",
			"v", "variadic",
		},
	}
	for _, c := range cases {
		dir := t.TempDir()
		path := writeTempGo(t, dir, "f.go", c.src)
		before := readFile(t, path)
		err := inlineCommand([]string{path, c.fn})
		if err == nil {
			t.Errorf("%s: expected refusal", c.name)
			continue
		}
		if code := exitCodeFor(err); code != exitParseError && code != exitNotFound {
			t.Errorf("%s: exit code = %d, want 2 or 3 (err: %v)", c.name, code, err)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error should mention %q, got: %v", c.name, c.want, err)
		}
		if readFile(t, path) != before {
			t.Errorf("%s: refusal must not modify the file", c.name)
		}
	}
}

func TestInlineRefusesImpureArgument(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go",
		"package x\n\nfunc id(n int) int {\n\treturn n\n}\n\nfunc gen() int { return 4 }\n\nfunc use() int {\n\treturn id(gen())\n}\n")

	err := inlineCommand([]string{path, "id"})
	assertExitCode(t, err, exitParseError)
	if !strings.Contains(err.Error(), "side effects") {
		t.Fatalf("error should explain the impure argument: %v", err)
	}
}

func TestInlineMissingFunction(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\nfunc Present() {}\n")

	err := inlineCommand([]string{path, "Absent"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), "Present") {
		t.Fatalf("error should list candidates: %v", err)
	}
}

func TestInlineRefusesMethodLocator(t *testing.T) {
	err := inlineCommand([]string{"f.go", "T:Method"})
	assertExitCode(t, err, exitUsage)
}
