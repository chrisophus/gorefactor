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
