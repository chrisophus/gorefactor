package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStringConcatInLoopRule(t *testing.T) {
	src := `package p

import "fmt"

func concat(items []string) string {
	s := ""
	for _, it := range items {
		s += "item: " + it
		s = s + fmt.Sprintf("%v", it)
	}
	total := 0
	for i := 0; i < 10; i++ {
		total += i // numeric: not flagged
	}
	return s
}
`
	issues := stringConcatInLoopRule{}.Run(writeLintFixture(t, "p.go", src))
	if len(issues) != 2 {
		t.Fatalf("issues = %+v, want 2 string concats (numeric += exempt)", issues)
	}
	for _, iss := range issues {
		if !strings.Contains(iss.Message, "strings.Builder") {
			t.Errorf("message should suggest strings.Builder: %q", iss.Message)
		}
	}
}

func TestLinearSearchInLoopRule(t *testing.T) {
	src := `package p

type user struct{ id int }

func match(orders []int, users []user) int {
	n := 0
	for _, o := range orders {
		for _, u := range users {
			if u.id == o {
				n++
			}
		}
	}
	for _, u := range users { // single loop: not flagged
		if u.id == 7 {
			n++
		}
	}
	return n
}
`
	issues := linearSearchInLoopRule{}.Run(writeLintFixture(t, "p.go", src))
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want exactly the nested scan", issues)
	}
	if !strings.Contains(issues[0].Message, "map") {
		t.Errorf("message should suggest a map: %q", issues[0].Message)
	}
}

func TestUnstoppedTickerRule(t *testing.T) {
	src := `package p

import "time"

func leaky() {
	t := time.NewTicker(time.Second)
	<-t.C
}

func fine() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	<-t.C
}

func transferred() *time.Ticker {
	t := time.NewTicker(time.Second)
	return t
}
`
	issues := unstoppedTickerRule{}.Run(writeLintFixture(t, "p.go", src))
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "leaky") {
		t.Fatalf("issues = %+v, want only the leaky ticker", issues)
	}
}

func TestNakedGoroutineRule(t *testing.T) {
	src := `package p

import "sync"

func naked() {
	go func() {
		println("fire and forget")
	}()
}

func managed() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
	}()
	wg.Wait()
}
`
	issues := nakedGoroutineRule{}.Run(writeLintFixture(t, "p.go", src))
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "naked") {
		t.Fatalf("issues = %+v, want only the unmanaged goroutine", issues)
	}
	if issues[0].Severity != "info" {
		t.Errorf("severity = %s, want info (heuristic rule)", issues[0].Severity)
	}
}

func TestConcRulesExemptMainPackage(t *testing.T) {
	src := `package main

import "time"

func main() {
	t := time.NewTicker(time.Second)
	go func() { <-t.C }()
	select {}
}
`
	ctx := writeLintFixture(t, "main.go", src)
	tickerIssues := unstoppedTickerRule{}.Run(ctx)
	if len(tickerIssues) != 0 {
		t.Fatalf("main package is exempt: %+v", tickerIssues)
	}
	goroutineIssues := nakedGoroutineRule{}.Run(ctx)
	if len(goroutineIssues) != 0 {
		t.Fatalf("main package is exempt: %+v", goroutineIssues)
	}
}

func TestPassThroughParamRule(t *testing.T) {
	src := `package p

func layer1(cfg string, n int) int { return layer2(cfg, n) }

func layer2(cfg string, n int) int { return layer3(cfg, n*2) }

func layer3(cfg string, n int) int { return layer4(cfg) + n }

func layer4(cfg string) int { return len(cfg) }
`
	issues := passThroughParamRule{}.Run(writeLintFixture(t, "p.go", src))
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want exactly one (cfg drilled from layer1; n is used at every layer)", issues)
	}
	iss := issues[0]
	if !strings.Contains(iss.Message, `"cfg"`) || !strings.Contains(iss.Message, "layer1") {
		t.Errorf("finding should name cfg at layer1: %q", iss.Message)
	}
	if iss.Severity != "info" {
		t.Errorf("severity = %s, want info", iss.Severity)
	}
}

func TestPassThroughParamRule_UsedParamNotFlagged(t *testing.T) {
	src := `package p

func a(v string) string { return b(v) }

func b(v string) string { return c(v) }

func c(v string) string { return v + "!" }
`
	issues := passThroughParamRule{}.Run(writeLintFixture(t, "p.go", src))
	if len(issues) != 0 {
		t.Fatalf("issues = %+v, want none (only 2 forwarding layers before use)", issues)
	}
}

func writeLintFixture(t *testing.T, name, src string) LintContext {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return LintContext{Root: dir, Files: []string{path}}
}
