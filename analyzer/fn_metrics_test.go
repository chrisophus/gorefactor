package analyzer

import "testing"

const fnMetricsSrc = `package x

func Small() int { return 1 }

type T struct{}

func (t *T) Deep(items []int) int {
	total := 0
	for _, i := range items {
		if i > 0 {
			switch {
			case i > 10:
				for j := 0; j < i; j++ {
					if j%2 == 0 {
						total += j
					}
				}
			default:
				total += i
			}
		}
	}
	return total
}
`

func TestFunctionMetricsForSource(t *testing.T) {
	metrics, err := FunctionMetricsForSource("x.go", []byte(fnMetricsSrc))
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 2 {
		t.Fatalf("got %d functions, want 2", len(metrics))
	}

	small := metrics[0]
	if small.Name != "Small" || small.Receiver != "" {
		t.Fatalf("unexpected first function: %+v", small)
	}
	if small.Lines != 1 || small.MaxNesting != 0 || small.Complexity != 1 {
		t.Fatalf("Small metrics: %+v", small)
	}
	if small.Key() != "Small" {
		t.Fatalf("Key() = %q", small.Key())
	}

	deep := metrics[1]
	if deep.Receiver != "T" || deep.Name != "Deep" {
		t.Fatalf("unexpected method: %+v", deep)
	}
	if deep.Key() != "T:Deep" {
		t.Fatalf("Key() = %q", deep.Key())
	}
	// for > if > switch > for > if = depth 5
	if deep.MaxNesting != 5 {
		t.Fatalf("Deep MaxNesting = %d, want 5", deep.MaxNesting)
	}
	if deep.Complexity < 5 {
		t.Fatalf("Deep Complexity = %d, want >= 5", deep.Complexity)
	}
	if deep.Lines < 15 {
		t.Fatalf("Deep Lines = %d, want >= 15", deep.Lines)
	}
}

func TestFunctionMetricsForSourceParseError(t *testing.T) {
	if _, err := FunctionMetricsForSource("bad.go", []byte("not go")); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLogicLines_ExcludesDataLiterals(t *testing.T) {
	src := []byte(`package p
func catalog() []T {
	return []T{
		{A: 1},
		{A: 2},
		{A: 3},
		{A: 4},
		{A: 5},
	}
}
func logic(n int) int {
	x := 0
	for i := 0; i < n; i++ {
		x += i
	}
	return x
}`)
	metrics, err := FunctionMetricsForSource("p.go", src)
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	byName := map[string]FunctionMetrics{}
	for _, m := range metrics {
		byName[m.Name] = m
	}
	cat := byName["catalog"]
	if cat.LiteralLines < 5 {
		t.Errorf("catalog LiteralLines = %d, want >=5", cat.LiteralLines)
	}
	if cat.LogicLines() > 3 {
		t.Errorf("catalog LogicLines = %d, want small (data, not logic)", cat.LogicLines())
	}
	if lg := byName["logic"]; lg.LiteralLines != 0 {
		t.Errorf("logic LiteralLines = %d, want 0", lg.LiteralLines)
	}
}
