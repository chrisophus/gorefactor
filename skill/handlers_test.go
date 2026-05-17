package main

import (
	"testing"
)

func TestHandleAnalyze(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantSuccess bool
	}{
		{
			name:        "analyze with no args",
			args:        []string{},
			wantSuccess: false,
		},
		{
			name:        "analyze non-existent file",
			args:        []string{"nonexistent.go"},
			wantSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := handleAnalyze(tt.args)

			if tt.wantSuccess {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if !result.Success {
					t.Error("expected success=true")
				}
			} else {
				if result.Success {
					t.Error("expected success=false")
				}
			}
		})
	}
}

func TestHandleExtract(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantSuccess bool
		wantErr     string
	}{
		{
			name:        "extract with no args",
			args:        []string{},
			wantSuccess: false,
			wantErr:     "Usage:",
		},
		{
			name:        "extract with invalid start line",
			args:        []string{"test.go", "abc", "20", "newFunc"},
			wantSuccess: false,
			wantErr:     "Invalid startLine",
		},
		{
			name:        "extract with invalid end line",
			args:        []string{"test.go", "10", "xyz", "newFunc"},
			wantSuccess: false,
			wantErr:     "Invalid endLine",
		},
		{
			name:        "extract with invalid range",
			args:        []string{"test.go", "20", "10", "newFunc"},
			wantSuccess: false,
			wantErr:     "Invalid line range",
		},
		{
			name:        "extract with zero line",
			args:        []string{"test.go", "0", "10", "newFunc"},
			wantSuccess: false,
			wantErr:     "Invalid line range",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := handleExtract(tt.args)

			if tt.wantSuccess {
				if !result.Success {
					t.Error("expected success=true")
				}
			} else {
				if result.Success {
					t.Error("expected success=false")
				}
				if tt.wantErr != "" && !contains(result.Message, tt.wantErr) {
					t.Errorf("message %q should contain %q", result.Message, tt.wantErr)
				}
			}
		})
	}
}

func TestHandleSuggest(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantSuccess bool
	}{
		{
			name:        "suggest with no args",
			args:        []string{},
			wantSuccess: false,
		},
		{
			name:        "suggest non-existent file",
			args:        []string{"nonexistent.go"},
			wantSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := handleSuggest(tt.args)

			if tt.wantSuccess {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if !result.Success {
					t.Error("expected success=true")
				}
			} else {
				if result.Success {
					t.Error("expected success=false")
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
