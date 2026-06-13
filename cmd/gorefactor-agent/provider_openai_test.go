package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestOpenAIDoWithRetry_SuccessOnFirst verifies that a 200 response on the
// first attempt is returned immediately with no retries.
func TestOpenAIDoWithRetry_SuccessOnFirst(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "model")
	status, body, err := p.doWithRetry(context.Background(), srv.URL+"/test", []byte(`{}`))
	if err != nil {
		t.Fatalf("doWithRetry: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !strings.Contains(string(body), "ok") {
		t.Fatalf("body = %q", body)
	}
	if calls != 1 {
		t.Fatalf("expected 1 HTTP call, got %d", calls)
	}
}

// TestOpenAIDoWithRetry_RetriesOn429 verifies the provider retries on rate-limit
// responses and succeeds once the server stops returning 429.
func TestOpenAIDoWithRetry_RetriesOn429(t *testing.T) {
	prev := retrySleep
	retrySleep = func(_ context.Context, _ time.Duration) error { return nil }
	defer func() { retrySleep = prev }()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"error":"rate limited"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "model")
	status, _, err := p.doWithRetry(context.Background(), srv.URL+"/test", []byte(`{}`))
	if err != nil {
		t.Fatalf("doWithRetry: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 after retries", status)
	}
	if calls != 3 {
		t.Fatalf("expected 3 HTTP calls (2 rate-limited + 1 success), got %d", calls)
	}
}

// TestOpenAIDoWithRetry_RetriesOn500 verifies retries on server errors.
func TestOpenAIDoWithRetry_RetriesOn500(t *testing.T) {
	prev := retrySleep
	retrySleep = func(_ context.Context, _ time.Duration) error { return nil }
	defer func() { retrySleep = prev }()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"error":"server error"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "model")
	status, _, err := p.doWithRetry(context.Background(), srv.URL+"/test", []byte(`{}`))
	if err != nil {
		t.Fatalf("doWithRetry: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls (1 error + 1 success), got %d", calls)
	}
}

// TestOpenAIDoWithRetry_ExhaustsAttempts verifies that after maxAttempts the
// error is returned with a descriptive message.
func TestOpenAIDoWithRetry_ExhaustsAttempts(t *testing.T) {
	prev := retrySleep
	retrySleep = func(_ context.Context, _ time.Duration) error { return nil }
	defer func() { retrySleep = prev }()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":"still limited"}`)
	}))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "model")
	_, _, err := p.doWithRetry(context.Background(), srv.URL+"/test", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error after exhausting all attempts, got nil")
	}
	if !strings.Contains(err.Error(), "failed after") {
		t.Errorf("error message should mention retry exhaustion: %v", err)
	}
	if calls != 5 {
		t.Fatalf("expected exactly 5 attempts, got %d", calls)
	}
}

// TestOpenAIDoWithRetry_ContextCancel verifies that cancelling the context
// aborts the retry loop before the next sleep completes.
func TestOpenAIDoWithRetry_ContextCancel(t *testing.T) {
	// Install a real-time sleep to make the cancel actually race.
	// retrySleep selects on ctx.Done() so cancellation is immediate.
	prev := retrySleep
	retrySleep = func(ctx context.Context, _ time.Duration) error {
		select {
		case <-time.After(10 * time.Second):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer func() { retrySleep = prev }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":"rate limited"}`)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	p := newOpenAIProvider(srv.URL, "key", "model")

	done := make(chan error, 1)
	go func() {
		_, _, err := p.doWithRetry(ctx, srv.URL+"/test", []byte(`{}`))
		done <- err
	}()

	// first attempt hits 429, then context is cancelled before the sleep resolves
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error on context cancel, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("doWithRetry did not respect context cancellation")
	}
}

// TestOpenAI_ChatToolsUsesRetry is an integration check: ChatTools returns an
// error when the server persistently returns 429, verifying the full call path
// goes through doWithRetry.
func TestOpenAI_ChatToolsUsesRetry(t *testing.T) {
	prev := retrySleep
	retrySleep = func(_ context.Context, _ time.Duration) error { return nil }
	defer func() { retrySleep = prev }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":"rate limited"}`)
	}))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "model")
	_, err := p.ChatTools(context.Background(), []chatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error on persistent 429, got nil")
	}
}
