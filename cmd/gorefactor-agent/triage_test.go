package main

import "testing"

// Triage matcher / guard unit tests added in response to Copilot review
// on PR #12: the regex-driven routing decisions are easy to break and
// were uncovered by tests until now. runTriaged end-to-end (chdir + git
// reset + applyOp dispatch) needs a tempdir module fixture and is left
// to a follow-up; these matcher tests catch the most common regression
// (a regex change that silently breaks one routing class).

func TestMatchRename(t *testing.T) {
	cases := []struct {
		name             string
		spec             string
		wantOK           bool
		wantOld, wantNew string
	}{
		{
			name:    "battery rename spec",
			spec:    "Rename the unexported function camelToSnake to camelToSnakeCase in package cmd/gorefactor and update all references.",
			wantOK:  true,
			wantOld: "camelToSnake", wantNew: "camelToSnakeCase",
		},
		{
			name:    "adjacent idents",
			spec:    "Rename foo to bar",
			wantOK:  true,
			wantOld: "foo", wantNew: "bar",
		},
		{
			name:   "no rename verb",
			spec:   "Move foo to bar.go",
			wantOK: false,
		},
		{
			name:   "identical old==new",
			spec:   "Rename foo to foo",
			wantOK: false,
		},
		{
			name:   "rename keyword absent",
			spec:   "Refactor parsing logic for clarity",
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			op, args, ok := matchRename(c.spec)
			if ok != c.wantOK {
				t.Fatalf("ok=%v, want %v (op=%q args=%v)", ok, c.wantOK, op, args)
			}
			if !ok {
				return
			}
			if op != "rename_declaration" {
				t.Errorf("op=%q, want rename_declaration", op)
			}
			if got, _ := args["function"].(string); got != c.wantOld {
				t.Errorf("function=%q, want %q", got, c.wantOld)
			}
			if got, _ := args["new_name"].(string); got != c.wantNew {
				t.Errorf("new_name=%q, want %q", got, c.wantNew)
			}
		})
	}
}

func TestMatchCallers(t *testing.T) {
	cases := []struct {
		name    string
		spec    string
		wantOK  bool
		wantSym string
	}{
		{
			name:    "battery analysis spec",
			spec:    "List the files and line numbers of every caller of the function emitRunMetrics. Do not modify any code; report the answer.",
			wantOK:  true,
			wantSym: "emitRunMetrics",
		},
		{
			name:    "who calls",
			spec:    "Who calls Foo?",
			wantOK:  true,
			wantSym: "Foo",
		},
		{
			name:    "callers of with method filler",
			spec:    "callers of the method Bar",
			wantOK:  true,
			wantSym: "Bar",
		},
		{
			name:   "unrelated",
			spec:   "Rename foo to bar",
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			op, args, ok := matchCallers(c.spec)
			if ok != c.wantOK {
				t.Fatalf("ok=%v, want %v (op=%q args=%v)", ok, c.wantOK, op, args)
			}
			if !ok {
				return
			}
			if op != "find_references_report" {
				t.Errorf("op=%q, want find_references_report (relabeled per Copilot review — senseFindRefs is substring grep, not strict call analysis)", op)
			}
			if got, _ := args["symbol"].(string); got != c.wantSym {
				t.Errorf("symbol=%q, want %q", got, c.wantSym)
			}
		})
	}
}

func TestMatchInfeasible(t *testing.T) {
	positives := []string{
		"Rewrite the duplicate-block detection in the analyzer package to use a rolling hash for linear-time performance.",
		"Optimize the parser for memory allocation",
		"Refactor the orchestrator design for better performance",
		"Fix the race condition in the queue worker",
		"Fix the goroutine leak in the dispatcher",
		"Redesign the dispatch approach",
		"Reimplement the loop with a different algorithm",
	}
	negatives := []string{
		"Rename foo to bar",
		"Move foo to helpers.go",
		"Callers of foo",
		"Create a new file at path/x.go",
		"Add a comment to function Foo",
	}
	for _, s := range positives {
		if _, _, ok := matchInfeasible(s); !ok {
			t.Errorf("expected match, got none for spec: %q", s)
		}
	}
	for _, s := range negatives {
		if _, _, ok := matchInfeasible(s); ok {
			t.Errorf("expected NO match, got one for spec: %q", s)
		}
	}
}

func TestHasNegativeConstraint(t *testing.T) {
	cases := []struct {
		name string
		spec string
		want bool
	}{
		{
			name: "do not rename named symbol",
			spec: "Rename Foo to Bar. Do NOT rename Baz.",
			want: true,
		},
		{
			name: "leave Y untouched",
			spec: "Rename Foo to Bar; leave Baz untouched.",
			want: true,
		},
		{
			name: "only on receiver",
			spec: "Rename Tokens to TokenUsage only on the anthropic provider.",
			want: true,
		},
		{
			name: "without modifying",
			spec: "Add ctx to foo without modifying the callers.",
			want: true,
		},
		{
			name: "plain mechanical, no constraint",
			spec: "Move camelToSnake to cmd/gorefactor/case_convert.go in the same package.",
			want: false,
		},
		{
			// Documents a v1 over-eagerness: "Do not change anything else"
			// is polite filler, not a meaningful constraint, but the v1
			// guard treats any "\bdo not\b" as a constraint cue. A
			// follow-up tightening (require an uppercase ident in the
			// constraint object) would flip this expectation to false.
			name: "v1 false positive — polite filler 'do not change anything else'",
			spec: "Move foo to helpers.go. Do not change anything else.",
			want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasNegativeConstraint(c.spec); got != c.want {
				t.Errorf("hasNegativeConstraint(%q) = %v, want %v", c.spec, got, c.want)
			}
		})
	}
}
