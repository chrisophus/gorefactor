package analyzer

import (
	"strings"
	"testing"
)

// ---- NewPlanSuggester ----

func TestNewPlanSuggester_ValidFile(t *testing.T) {
	t.Parallel()
	path := writePlanSuggesterFile(t, "package p\n\nfunc F() {}\n")
	ps, err := NewPlanSuggester(path)
	if err != nil {
		t.Fatalf("NewPlanSuggester: %v", err)
	}
	if ps == nil {
		t.Fatal("expected non-nil PlanSuggester")
	}
	if ps.File == nil {
		t.Error("expected non-nil parsed File")
	}
}

func TestNewPlanSuggester_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := NewPlanSuggester("/nonexistent/path/file.go")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestNewPlanSuggester_InvalidGo(t *testing.T) {
	t.Parallel()
	path := writePlanSuggesterFile(t, "this is not go code {{{")
	_, err := NewPlanSuggester(path)
	if err == nil {
		t.Fatal("expected error for invalid Go syntax")
	}
}

// ---- SuggestExtractions ----

func TestSuggestExtractions_SimpleFunction_NoSuggestion(t *testing.T) {
	t.Parallel()
	// A tiny function with complexity < 10; should not be flagged.
	src := `package p

func Simple() error {
	return nil
}
`
	ps, err := NewPlanSuggester(writePlanSuggesterFile(t, src))
	if err != nil {
		t.Fatal(err)
	}
	plans := ps.SuggestExtractions()
	if len(plans) != 0 {
		t.Errorf("expected no extraction suggestions for simple function, got %d", len(plans))
	}
}

func TestSuggestExtractions_ComplexFunction_ReturnsSuggestion(t *testing.T) {
	t.Parallel()
	// Construct a function with many statements and control structures to push
	// complexity >= 10 (score = 1 + stmts + control structures).
	src := `package p

func Complex(x int) int {
	if x > 0 {
		x++
	}
	if x > 1 {
		x++
	}
	if x > 2 {
		x++
	}
	if x > 3 {
		x++
	}
	if x > 4 {
		x++
	}
	return x
}
`
	ps, err := NewPlanSuggester(writePlanSuggesterFile(t, src))
	if err != nil {
		t.Fatal(err)
	}
	plans := ps.SuggestExtractions()
	if len(plans) == 0 {
		t.Fatal("expected at least one extraction suggestion for complex function")
	}
	found := false
	for _, p := range plans {
		if strings.Contains(p.Name, "Complex") {
			found = true
			if len(p.Operations) == 0 {
				t.Error("extraction plan has no operations")
			}
			if p.SafetyRisk != "low" {
				t.Errorf("expected SafetyRisk=low, got %q", p.SafetyRisk)
			}
		}
	}
	if !found {
		t.Errorf("expected suggestion for 'Complex', got plans: %+v", plans)
	}
}

// ---- SuggestRenames ----

func TestSuggestRenames_ExportedFunction_NoSuggestion(t *testing.T) {
	t.Parallel()
	src := `package p

// Exported does something.
func Exported() {}
`
	ps, err := NewPlanSuggester(writePlanSuggesterFile(t, src))
	if err != nil {
		t.Fatal(err)
	}
	plans := ps.SuggestRenames()
	if len(plans) != 0 {
		t.Errorf("expected no rename suggestions for already-exported function, got %d", len(plans))
	}
}

func TestSuggestRenames_UnexportedWithDoc_ReturnsSuggestion(t *testing.T) {
	t.Parallel()
	// unexported function with a doc comment and no private/internal prefix.
	src := `package p

// helper does something useful.
func helper() {}
`
	ps, err := NewPlanSuggester(writePlanSuggesterFile(t, src))
	if err != nil {
		t.Fatal(err)
	}
	plans := ps.SuggestRenames()
	if len(plans) == 0 {
		t.Fatal("expected at least one rename suggestion for documented unexported function")
	}
	if !strings.Contains(plans[0].Name, "helper") {
		t.Errorf("expected suggestion mentioning 'helper', got: %+v", plans[0])
	}
	if plans[0].SafetyRisk != "medium" {
		t.Errorf("expected SafetyRisk=medium, got %q", plans[0].SafetyRisk)
	}
}

func TestSuggestRenames_UnexportedWithPrivatePrefix_NoSuggestion(t *testing.T) {
	t.Parallel()
	// "private" prefix should suppress the rename suggestion.
	src := `package p

// privateHelper is deliberately private.
func privateHelper() {}
`
	ps, err := NewPlanSuggester(writePlanSuggesterFile(t, src))
	if err != nil {
		t.Fatal(err)
	}
	plans := ps.SuggestRenames()
	if len(plans) != 0 {
		t.Errorf("expected no rename suggestion for private-prefixed function, got %d", len(plans))
	}
}

func TestSuggestRenames_UnexportedWithInternalPrefix_NoSuggestion(t *testing.T) {
	t.Parallel()
	src := `package p

// internalHelper is internal.
func internalHelper() {}
`
	ps, err := NewPlanSuggester(writePlanSuggesterFile(t, src))
	if err != nil {
		t.Fatal(err)
	}
	plans := ps.SuggestRenames()
	if len(plans) != 0 {
		t.Errorf("expected no rename suggestion for internal-prefixed function, got %d", len(plans))
	}
}

// ---- SuggestReorganization ----

func TestSuggestReorganization_SmallFile_NoSuggestion(t *testing.T) {
	t.Parallel()
	src := `package p

func F() {}
`
	ps, err := NewPlanSuggester(writePlanSuggesterFile(t, src))
	if err != nil {
		t.Fatal(err)
	}
	plans := ps.SuggestReorganization()
	if len(plans) != 0 {
		t.Errorf("expected no reorganization suggestion for small file, got %d", len(plans))
	}
}

func TestSuggestReorganization_LargeFile_ReturnsSuggestion(t *testing.T) {
	t.Parallel()
	// Build a file with > 500 lines.
	var sb strings.Builder
	sb.WriteString("package p\n\n")
	for i := range 60 {
		sb.WriteString("// F is a function\n")
		sb.WriteString("func F")
		for j := 0; j < i+1; j++ {
			sb.WriteString("x")
		}
		sb.WriteString("() {\n")
		// 7 lines of body to push line count up
		sb.WriteString("\ta := 1\n\tb := 2\n\tc := a + b\n\td := c * 2\n\te := d - 1\n\tf := e + 1\n\t_ = f\n")
		sb.WriteString("}\n\n")
	}

	ps, err := NewPlanSuggester(writePlanSuggesterFile(t, sb.String()))
	if err != nil {
		t.Fatal(err)
	}
	plans := ps.SuggestReorganization()
	if len(plans) == 0 {
		t.Fatal("expected reorganization suggestion for large file")
	}
	if !strings.Contains(plans[0].Name, "Split") {
		t.Errorf("expected 'Split' in plan name, got %q", plans[0].Name)
	}
}

// ---- AllSuggestions ----

func TestAllSuggestions_CombinesAllTypes(t *testing.T) {
	t.Parallel()
	// A complex function that will trigger extraction suggestions.
	src := `package p

func Complex(x int) int {
	if x > 0 { x++ }
	if x > 1 { x++ }
	if x > 2 { x++ }
	if x > 3 { x++ }
	if x > 4 { x++ }
	return x
}
`
	ps, err := NewPlanSuggester(writePlanSuggesterFile(t, src))
	if err != nil {
		t.Fatal(err)
	}
	// AllSuggestions should return at least the extraction suggestions.
	all := ps.AllSuggestions()
	extr := ps.SuggestExtractions()
	if len(all) < len(extr) {
		t.Errorf("AllSuggestions (%d) should be >= SuggestExtractions (%d)", len(all), len(extr))
	}
}

// ---- SuggestedPlan helpers ----

func TestSuggestedPlan_ToJSON_ValidJSON(t *testing.T) {
	t.Parallel()
	plan := SuggestedPlan{
		Name:        "Test plan",
		Description: "a test",
		Complexity:  "low",
		SafetyRisk:  "low",
		Rationale:   "testing",
		Operations: []SuggestedOp{
			{Type: "extract_method", Description: "extract", Priority: 5},
		},
	}
	j, err := plan.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	if !strings.Contains(j, "Test plan") {
		t.Errorf("JSON missing plan name: %s", j)
	}
	if !strings.Contains(j, "extract_method") {
		t.Errorf("JSON missing operation type: %s", j)
	}
}

func TestSuggestedPlan_Summary_ContainsFields(t *testing.T) {
	t.Parallel()
	plan := SuggestedPlan{
		Name:        "My Plan",
		Description: "desc",
		Complexity:  "high",
		SafetyRisk:  "medium",
	}
	s := plan.Summary()
	for _, want := range []string{"My Plan", "desc", "high", "medium"} {
		if !strings.Contains(s, want) {
			t.Errorf("Summary missing %q: %s", want, s)
		}
	}
}

// ---- complexityLevel ----

func TestComplexityLevel_Thresholds(t *testing.T) {
	t.Parallel()
	ps := &PlanSuggester{}
	cases := []struct {
		score int
		want  string
	}{
		{0, "low"},
		{4, "low"},
		{5, "medium"},
		{9, "medium"},
		{10, "high"},
		{100, "high"},
	}
	for _, c := range cases {
		got := ps.complexityLevel(c.score)
		if got != c.want {
			t.Errorf("complexityLevel(%d) = %q, want %q", c.score, got, c.want)
		}
	}
}

// ---- isExported / hasPrivatePrefix ----

func TestIsExported(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		want bool
	}{
		{"Exported", true},
		{"unexported", false},
		{"", false},
		{"A", true},
		{"z", false},
	}
	for _, c := range cases {
		if got := isExported(c.name); got != c.want {
			t.Errorf("isExported(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestHasPrivatePrefix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		want bool
	}{
		{"privateHelper", true},
		{"internalHelper", true},
		{"helper", false},
		{"myHelper", false},
		{"private", true},
		{"internal", true},
	}
	for _, c := range cases {
		if got := hasPrivatePrefix(c.name); got != c.want {
			t.Errorf("hasPrivatePrefix(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// writePlanSuggesterFile writes Go source to a temp file and returns the path.
func writePlanSuggesterFile(t *testing.T, src string) string {
	t.Helper()
	return writeTempGo(t, src)
}
