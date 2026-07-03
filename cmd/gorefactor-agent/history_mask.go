package main

import (
	"fmt"
	"strings"
)

// Phase 1: tool-output masking.
//
// Input tokens dominate agentic cost, and the loop re-sends the whole
// tool history every round. A tool result is only load-bearing for a
// few rounds after it is produced; after that it is stale context the
// model re-reads (and pays for) every turn. maskStaleToolOutputs
// replaces the body of every tool result older than the last N
// assistant turns with a one-line structured stub, leaving the raw
// transcript on disk untouched (this transform runs at prompt-assembly
// time only, so the audit trail and the Phase 6 corpus are unaffected).
//
// Masking rule, single and unambiguous: a tool result is masked iff it
// is older than the last maskAfterRounds assistant turns. Recency is
// the sole trigger. The task objective (a user message) and any
// most-recent error are never tool-role messages older than the window,
// so the recency cutoff alone honours the never-mask list.
const maskAfterRounds = 8

// maskStaleToolOutputs stubs tool-role message bodies that fall before
// the keepRounds-th assistant turn counted from the end. Message count
// and ordering are preserved, so the "a tool message always follows its
// triggering tool_call" invariant that providers require is never
// broken.
func maskStaleToolOutputs(msgs []chatMessage, keepRounds int) []chatMessage {
	if keepRounds < 1 {
		keepRounds = 1
	}
	// cutoff = index of the keepRounds-th assistant message from the end.
	// Everything strictly before it is stale and eligible for masking.
	seen, cutoff := 0, -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			seen++
			if seen == keepRounds {
				cutoff = i
				break
			}
		}
	}
	if cutoff <= 0 {
		return msgs // fewer than keepRounds rounds; nothing is stale yet
	}

	// Map each tool_call ID to the tool name so the stub can identify
	// which tool's output was elided.
	names := map[string]string{}
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			names[tc.ID] = tc.Function.Name
		}
	}

	out := make([]chatMessage, len(msgs))
	copy(out, msgs)
	for i := 0; i < cutoff; i++ {
		if out[i].Role != "tool" || out[i].Content == "" {
			continue
		}
		if strings.HasPrefix(out[i].Content, maskMarker) {
			continue // already masked (idempotent under repeated assembly)
		}
		out[i].Content = maskStub(names[out[i].ToolCallID], out[i].Content)
	}
	return out
}

// assembleHistory is the single prompt-assembly entry point every driver call site uses: compact,
// then mask. Note that the two compose the same way regardless of order -- both key off distance
// from the end of the list (compaction trims only the front; masking counts assistant turns from
// the back), so masking runs on whatever tail compaction leaves, every round, for the life of a
// long conversation. This helper exists to keep that composition in one place rather than three
// call sites.
func assembleHistory(msgs []chatMessage, keep int) []chatMessage {
	return maskStaleToolOutputs(compactMessages(msgs, keep), maskAfterRounds)
}

const maskMarker = "[elided"

// maskStub is the one-line replacement for a stale tool result: the tool
// name, the original size, and a short digest of the first line so the
// model retains a breadcrumb of what happened without paying for the
// full body every round.
func maskStub(name, content string) string {
	if name == "" {
		name = "tool"
	}
	return fmt.Sprintf("%s: %s result, %d bytes; %s]",
		maskMarker, name, len(content), trim(firstLine(content), 80))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
