package main

import (
	"path/filepath"
	"testing"
)

// TestDestFileFor_NoTokenStutter is harness-integrity plan item 2's
// acceptance test: splitting a file named after its dominant receiver must
// never compose a filename that repeats a token. The provider fixture is the
// historical failure — splitting provider_anthropic.go by receiver
// AnthropicProvider minted provider_anthropic_anthropic_provider.go.
func TestDestFileFor_NoTokenStutter(t *testing.T) {
	cases := []struct {
		name string
		stem string
		key  string
		want string
	}{
		{"provider fixture: suffix wholly contained in stem", "provider_anthropic", "recv:AnthropicProvider", "provider_anthropic_methods.go"},
		{"identical stem and receiver", "orchestrator", "recv:Orchestrator", "orchestrator_methods.go"},
		{"partial overlap keeps only the new token", "provider_anthropic", "recv:AnthropicStream", "provider_anthropic_stream.go"},
		{"no overlap is unchanged", "types", "recv:Parser", "types_parser.go"},
		{"prefix group with full overlap falls back to part", "split_plan", "prefix:splitPlan", "split_plan_part.go"},
		{"test file keeps the _test suffix after dedupe", "provider_anthropic_test", "recv:AnthropicProvider", "provider_anthropic_methods_test.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := destFileFor("dir", tc.stem, splitGroup{key: tc.key}, map[string]bool{})
			if got != filepath.Join("dir", tc.want) {
				t.Errorf("destFileFor(%q, %q) = %q, want %q", tc.stem, tc.key, got, filepath.Join("dir", tc.want))
			}
		})
	}
}

// TestDestFileFor_CollisionSuffixStillApplies pins that the used-name
// collision counter survives the dedupe rewrite.
func TestDestFileFor_CollisionSuffixStillApplies(t *testing.T) {
	used := map[string]bool{}
	first := destFileFor("dir", "types", splitGroup{key: "recv:Parser"}, used)
	second := destFileFor("dir", "types", splitGroup{key: "recv:Parser"}, used)
	if first == second {
		t.Fatalf("collision not resolved: both %q", first)
	}
	if want := filepath.Join("dir", "types_parser2.go"); second != want {
		t.Errorf("second candidate = %q, want %q", second, want)
	}
}

func TestDropStemTokens(t *testing.T) {
	cases := []struct{ stem, suffix, want string }{
		{"provider_anthropic", "anthropic_provider", ""},
		{"provider_anthropic", "anthropic_stream", "stream"},
		{"types", "parser", "parser"},
		{"types", "", ""},
	}
	for _, tc := range cases {
		if got := dropStemTokens(tc.stem, tc.suffix); got != tc.want {
			t.Errorf("dropStemTokens(%q, %q) = %q, want %q", tc.stem, tc.suffix, got, tc.want)
		}
	}
}
