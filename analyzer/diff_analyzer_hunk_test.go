package analyzer

import "testing"

// TestAnalyzeHunk_TwoEditHunkYieldsTwoChanges is harness-integrity plan item
// 8's acceptance criterion: a single hunk containing two separate edits
// (distinct -/+ runs separated by context) must produce two changes, where
// analyzeHunk previously collapsed the whole hunk into its first
// modification.
func TestAnalyzeHunk_TwoEditHunkYieldsTwoChanges(t *testing.T) {
	diffContent := `diff --git a/service.go b/service.go
--- a/service.go
+++ b/service.go
@@ -10,7 +10,7 @@
 func process(items []string) {
-	count := 0
+	total := 0
 	for _, it := range items {
-		handle(it)
+		handleItem(it)
 	}
 }`

	da := NewDiffAnalyzer()
	analysis, err := da.AnalyzeDiffString(diffContent)
	if err != nil {
		t.Fatalf("AnalyzeDiffString() failed: %v", err)
	}
	if len(analysis.Changes) != 2 {
		t.Fatalf("expected 2 changes from a two-edit hunk, got %d: %+v", len(analysis.Changes), analysis.Changes)
	}
	// Per-run line numbers: first edit replaces new-side line 11, second
	// new-side line 13 — not the whole hunk's range.
	if analysis.Changes[0].StartLine != 11 || analysis.Changes[0].EndLine != 11 {
		t.Errorf("first change lines = %d-%d, want 11-11", analysis.Changes[0].StartLine, analysis.Changes[0].EndLine)
	}
	if analysis.Changes[1].StartLine != 13 || analysis.Changes[1].EndLine != 13 {
		t.Errorf("second change lines = %d-%d, want 13-13", analysis.Changes[1].StartLine, analysis.Changes[1].EndLine)
	}
}

// TestAnalyzeHunk_MixedRunsKeepTheirKinds pins that a hunk with a
// modification run, a pure addition run, and a pure removal run yields one
// change of each kind instead of only the modification.
func TestAnalyzeHunk_MixedRunsKeepTheirKinds(t *testing.T) {
	diffContent := `diff --git a/service.go b/service.go
--- a/service.go
+++ b/service.go
@@ -1,8 +1,8 @@
 package service
-var a = 1
+var a = 2
 var keep1 = 0
+var added = 3
 var keep2 = 0
-var removed = 4
 var keep3 = 0`

	da := NewDiffAnalyzer()
	analysis, err := da.AnalyzeDiffString(diffContent)
	if err != nil {
		t.Fatalf("AnalyzeDiffString() failed: %v", err)
	}
	if len(analysis.Changes) != 3 {
		t.Fatalf("expected 3 changes, got %d: %+v", len(analysis.Changes), analysis.Changes)
	}
	kinds := map[string]bool{}
	for _, c := range analysis.Changes {
		kinds[c.Type] = true
	}
	for _, want := range []string{"code_modification", "code_insertion", "code_removal"} {
		if !kinds[want] {
			t.Errorf("missing change kind %q in %v", want, kinds)
		}
	}
}

// TestSplitHunkRuns_OldSideRanges pins that old-side line numbers survive
// into the runs (the pre-fix parser discarded the hunk header's old-side
// range entirely).
func TestSplitHunkRuns_OldSideRanges(t *testing.T) {
	hunk := &DiffHunk{
		StartLine:    10,
		OldStartLine: 20,
		Lines: []string{
			" ctx",
			"-removed one",
			"-removed two",
			" ctx",
			"+added",
		},
	}
	runs := splitHunkRuns(hunk)
	if len(runs) != 2 {
		t.Fatalf("got %d runs, want 2: %+v", len(runs), runs)
	}
	if runs[0].oldStart != 21 || len(runs[0].oldLines) != 2 {
		t.Errorf("removal run: oldStart=%d oldLines=%d, want 21 and 2", runs[0].oldStart, len(runs[0].oldLines))
	}
	if h := runs[0].asHunk(); h.OldStartLine != 21 || h.OldEndLine != 22 {
		t.Errorf("removal run old range = %d-%d, want 21-22", h.OldStartLine, h.OldEndLine)
	}
	if runs[1].newStart != 12 {
		t.Errorf("addition run newStart = %d, want 12", runs[1].newStart)
	}
}
