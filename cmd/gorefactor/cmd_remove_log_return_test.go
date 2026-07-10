package main

import (
	"strings"
	"testing"

	"github.com/chrisophus/gorefactor/analyzer"
)

const removeLogReturnSrc = `package main

import (
	"fmt"
	"log/slog"
)

// wrap-log-return: wrap, log, return — the log must go, the wrap stays.
func SaveOrder(id string) error {
	if err := persistOrder(id); err != nil {
		err = fmt.Errorf("persist order: %w", err)
		slog.Error("persist failed", "err", err)
		return err
	}
	return nil
}

// if-err-log-return with a bare return: log deleted, return wrapped.
func LoadOrder(id string) error {
	if err := readOrder(id); err != nil {
		slog.Error("read failed", "err", err)
		return err
	}
	return nil
}

// if-err-log-return with an already-wrapped return: only the log goes.
func CheckOrder(id string) error {
	if err := readOrder(id); err != nil {
		slog.Error("check failed", "err", err)
		return fmt.Errorf("check order: %w", err)
	}
	return nil
}

// Non-adjacent log and return: not a safe fix, must be left alone.
func AuditOrder(id string) error {
	if err := readOrder(id); err != nil {
		slog.Error("audit failed", "err", err)
		id = ""
		return err
	}
	return nil
}

func persistOrder(id string) error { return nil }
func readOrder(id string) error    { return nil }

func main() {}
`

func TestRemoveLogReturnFixesAdjacentPatterns(t *testing.T) {
	writeModule(t, map[string]string{"main.go": removeLogReturnSrc})
	captureStdout(t, func() {
		if err := removeLogReturnCommand([]string{"main.go"}); err != nil {
			t.Fatalf("remove-log-return: %v", err)
		}
	})
	src := readFile(t, "main.go")

	if strings.Contains(src, `slog.Error("persist failed"`) {
		t.Errorf("wrap-log-return log should be deleted:\n%s", src)
	}
	if !strings.Contains(src, `fmt.Errorf("persist order: %w", err)`) {
		t.Errorf("wrap-log-return wrap must survive:\n%s", src)
	}
	if strings.Contains(src, `slog.Error("read failed"`) {
		t.Errorf("if-err-log-return log should be deleted:\n%s", src)
	}
	if !strings.Contains(src, `return fmt.Errorf("read order: %w", err)`) {
		t.Errorf("bare return err should be wrapped with call-derived context:\n%s", src)
	}
	if strings.Contains(src, `slog.Error("check failed"`) {
		t.Errorf("log before wrapped return should be deleted:\n%s", src)
	}
	if !strings.Contains(src, `return fmt.Errorf("check order: %w", err)`) {
		t.Errorf("already-wrapped return must be preserved:\n%s", src)
	}
	// The non-adjacent case stays untouched.
	if !strings.Contains(src, `slog.Error("audit failed"`) {
		t.Errorf("non-adjacent log/return must be left alone:\n%s", src)
	}
}

func TestRemoveLogReturnRuleFilter(t *testing.T) {
	writeModule(t, map[string]string{"main.go": removeLogReturnSrc})
	captureStdout(t, func() {
		if err := removeLogReturnCommand([]string{"main.go", "--rule", "wrap-log-return"}); err != nil {
			t.Fatalf("remove-log-return --rule: %v", err)
		}
	})
	src := readFile(t, "main.go")
	if strings.Contains(src, `slog.Error("persist failed"`) {
		t.Errorf("wrap-log-return site should be fixed:\n%s", src)
	}
	// if-err-log-return sites are out of scope for this rule filter.
	if !strings.Contains(src, `slog.Error("read failed"`) {
		t.Errorf("--rule wrap-log-return must not touch if-err-log-return sites:\n%s", src)
	}
}

func TestRemoveLogReturnIdempotent(t *testing.T) {
	writeModule(t, map[string]string{"main.go": removeLogReturnSrc})
	captureStdout(t, func() {
		if err := removeLogReturnCommand([]string{"main.go"}); err != nil {
			t.Fatalf("first run: %v", err)
		}
	})
	first := readFile(t, "main.go")
	out := captureStdout(t, func() {
		if err := removeLogReturnCommand([]string{"main.go"}); err != nil {
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

func TestRemoveLogReturnUnknownRule(t *testing.T) {
	writeModule(t, map[string]string{"main.go": removeLogReturnSrc})
	if err := removeLogReturnCommand([]string{"main.go", "--rule", "no-such-rule"}); err == nil {
		t.Fatal("unknown --rule must be rejected")
	}
}

func TestLogPropagationRulesAttachAutofix(t *testing.T) {
	writeModule(t, map[string]string{"main.go": removeLogReturnSrc})
	ctx := LintContext{Root: ".", Files: []string{"main.go"}, WalkOpts: analyzer.DefaultWalkOptions()}

	var fixable, unfixable int
	for _, iss := range (ifErrLogReturnRule{}).Run(ctx) {
		if iss.AutoFixCmd != "" {
			if want := "remove-log-return main.go --rule if-err-log-return"; iss.AutoFixCmd != want {
				t.Errorf("AutoFixCmd = %q, want %q", iss.AutoFixCmd, want)
			}
			fixable++
		} else {
			unfixable++
		}
	}
	if fixable != 2 || unfixable != 2 {
		t.Errorf("if-err-log-return: fixable=%d unfixable=%d, want 2/2", fixable, unfixable)
	}

	wrapIssues := (wrapLogReturnRule{}).Run(ctx)
	if len(wrapIssues) != 1 || wrapIssues[0].AutoFixCmd == "" {
		t.Errorf("wrap-log-return should flag SaveOrder with an autofix, got %+v", wrapIssues)
	}
}

func TestLogPropagationAutoFixEndToEnd(t *testing.T) {
	writeModule(t, map[string]string{"main.go": removeLogReturnSrc})
	ctx := LintContext{Root: ".", Files: []string{"main.go"}, WalkOpts: analyzer.DefaultWalkOptions()}
	rules := []LintRule{ifErrLogReturnRule{}, wrapLogReturnRule{}}
	var issues []lintIssue
	for _, r := range rules {
		issues = append(issues, r.Run(ctx)...)
	}
	applied, reverted, failed := applyAutoFixes(issues, ctx, rules, false)
	if failed > 0 || reverted > 0 {
		t.Fatalf("applyAutoFixes: applied=%d reverted=%d failed=%d", applied, reverted, failed)
	}
	if applied == 0 {
		t.Fatal("expected at least one applied fix")
	}
	src := readFile(t, "main.go")
	for _, gone := range []string{`"persist failed"`, `"read failed"`, `"check failed"`} {
		if strings.Contains(src, gone) {
			t.Errorf("log %s should be gone after --fix:\n%s", gone, src)
		}
	}
	if !strings.Contains(src, `slog.Error("audit failed"`) {
		t.Errorf("non-adjacent site must survive --fix:\n%s", src)
	}
}

func TestDuplicateBareSentinelAttachesAutofix(t *testing.T) {
	writeModule(t, map[string]string{"main.go": wrapSentinelsSrc})
	ctx := LintContext{Root: ".", Files: []string{"main.go"}, WalkOpts: analyzer.DefaultWalkOptions()}
	issues := (duplicateBareSentinelRule{}).Run(ctx)
	if len(issues) != 2 {
		t.Fatalf("expected 2 duplicate-bare-sentinel issues, got %d", len(issues))
	}
	for _, iss := range issues {
		if want := "wrap-sentinels main.go ErrNotFound"; iss.AutoFixCmd != want {
			t.Errorf("AutoFixCmd = %q, want %q", iss.AutoFixCmd, want)
		}
	}
	rules := []LintRule{duplicateBareSentinelRule{}}
	applied, _, failed := applyAutoFixes(issues, ctx, rules, false)
	if failed > 0 || applied == 0 {
		t.Fatalf("applyAutoFixes: applied=%d failed=%d", applied, failed)
	}
	if !strings.Contains(readFile(t, "main.go"), `fmt.Errorf("lookup user: %w", ErrNotFound)`) {
		t.Error("sentinel return not wrapped after --fix")
	}
}
