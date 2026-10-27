package agent

// compaction_test.go: ai-frontend-builder T19 (AFB-10). The history of earlier
// turns is seeded straight into the fake store; the system prompt and tool
// specs are ~10k chars, so the windows below are sized against that.

import (
	"errors"
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

const bigText = 6000 // chars per seeded message

func filler(tag string) string { return tag + " " + strings.Repeat("lorem ipsum ", bigText/12) }

// seedOldTurns adds n finished turns (user + assistant text) before turn-1.
func seedOldTurns(h *harness, n int) {
	// The harness seeded the current turn's user message first; history must
	// come before it.
	h.st.msgs, h.st.nextID = nil, 0
	for i := 1; i <= n; i++ {
		id := "old-" + string(rune('0'+i))
		h.st.seed(id, store.RoleUser, filler("USER-"+id))
		h.st.seed(id, store.RoleAssistant, filler("ASSISTANT-"+id))
	}
	h.st.seed("turn-1", store.RoleUser, "build me a tasks page")
}

func requestText(req provider.ChatRequest) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func TestCompaction_BelowThresholdLeavesHistoryUntouched(t *testing.T) {
	h := newHarness(t, scriptStep{resp: textResp("done")})
	seedOldTurns(h, 3)
	h.deps.Limits = Limits{ContextWindowTokens: 200000} // far above ~45k chars
	h.mustRun()

	if h.prov.calls() != 1 {
		t.Fatalf("provider calls = %d, want 1 (no summarization below the threshold)", h.prov.calls())
	}
	if got := len(h.st.messagesOf(store.RoleSummary)); got != 0 {
		t.Errorf("summary messages = %d, want 0", got)
	}
	req := h.prov.reqs[0]
	if len(req.Messages) != 7 { // 3 old turns x 2 + current user message
		t.Errorf("messages sent = %d, want the full history (7)", len(req.Messages))
	}
	if !strings.Contains(requestText(req), "USER-old-1") {
		t.Error("oldest turn missing from an uncompacted prompt")
	}
}

func TestCompaction_AboveThresholdSummarizesOlderTurnsAndKeepsRecentVerbatim(t *testing.T) {
	h := newHarness(t,
		scriptStep{resp: textResp("SUMMARY: user wants a tasks page; app scaffolded.")}, // the summarization call
		scriptStep{resp: textResp("done")},
	)
	seedOldTurns(h, 4)
	h.deps.Limits = Limits{ContextWindowTokens: 8000, KeepRecentTurns: 2} // budget ~25.6k chars < ~58k
	h.mustRun()

	if h.prov.calls() != 2 {
		t.Fatalf("provider calls = %d, want 2 (one summarization + the turn)", h.prov.calls())
	}
	sumReq, chatReq := h.prov.reqs[0], h.prov.reqs[1]
	if sumReq.System != summarySystemPrompt || len(sumReq.Tools) != 0 {
		t.Error("the first call must be the tool-less summarization call")
	}
	text := requestText(chatReq)
	if !strings.Contains(chatReq.Messages[0].Content, "SUMMARY: user wants a tasks page") {
		t.Errorf("first message = %q, want the summary", chatReq.Messages[0].Content[:60])
	}
	for _, gone := range []string{"USER-old-1", "ASSISTANT-old-2", "USER-old-3"} {
		if strings.Contains(text, gone) {
			t.Errorf("%s still in the prompt after compaction", gone)
		}
	}
	for _, kept := range []string{"USER-old-4", "ASSISTANT-old-4", "build me a tasks page"} {
		if !strings.Contains(text, kept) {
			t.Errorf("recent content %q must stay verbatim", kept)
		}
	}
	// The DB history is never rewritten: only a summary row is added.
	sums := h.st.messagesOf(store.RoleSummary)
	if len(sums) != 1 {
		t.Fatalf("summary rows = %d, want 1", len(sums))
	}
	meta, ok := parseSummaryMeta(sums[0])
	if !ok || meta.ThroughMessageID != 6 { // 3 summarized turns x 2 messages
		t.Errorf("summary covers through message %d (ok=%v), want 6", meta.ThroughMessageID, ok)
	}
	if len(h.st.usage) != 2 {
		t.Errorf("usage records = %d, want the summarization call recorded too (2)", len(h.st.usage))
	}
}

func TestCompaction_SummaryIsReusedOnTheNextTurn(t *testing.T) {
	h := newHarness(t,
		scriptStep{resp: textResp("SUMMARY-ONE")},
		scriptStep{resp: textResp("first turn done")},
		scriptStep{resp: textResp("second turn done")},
	)
	seedOldTurns(h, 4)
	h.deps.Limits = Limits{ContextWindowTokens: 8000, KeepRecentTurns: 2}
	h.mustRun()
	callsAfterFirst := h.prov.calls()

	// Next turn on the same session. The kept history plus the summary fits in
	// the window, so nothing must be summarized again.
	h.st.startTurn("turn-2", 2, "now add a delete button")
	h.deps.Limits = Limits{ContextWindowTokens: 12000, KeepRecentTurns: 2}
	h.mustRun()

	if got := h.prov.calls() - callsAfterFirst; got != 1 {
		t.Fatalf("second turn made %d provider calls, want 1 (summary reused, no second summarization)", got)
	}
	if got := len(h.st.messagesOf(store.RoleSummary)); got != 1 {
		t.Errorf("summary rows = %d, want still 1", got)
	}
	req := h.prov.reqs[len(h.prov.reqs)-1]
	if !strings.HasPrefix(req.Messages[0].Content, summaryPreamble) || !strings.Contains(req.Messages[0].Content, "SUMMARY-ONE") {
		t.Error("the stored summary must head the next prompt")
	}
	if strings.Contains(requestText(req), "USER-old-1") {
		t.Error("summarized turns must not come back into the prompt")
	}
}

func TestCompaction_ContextTooLongForcesACompactionAndRetries(t *testing.T) {
	h := newHarness(t,
		scriptStep{err: provider.ErrContextTooLong},    // the real call is rejected
		scriptStep{resp: textResp("SUMMARY-FORCED")},   // forced summarization
		scriptStep{resp: textResp("done after retry")}, // retry succeeds
	)
	seedOldTurns(h, 3)
	h.deps.Limits = Limits{ContextWindowTokens: 200000, KeepRecentTurns: 1} // estimate says fine
	h.mustRun()

	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q, want completed after the forced compaction", fin.Status)
	}
	if got := len(h.st.messagesOf(store.RoleSummary)); got != 1 {
		t.Errorf("summary rows = %d, want 1", got)
	}
}

func TestCompaction_SummarizationFailureFallsBackToFullHistory(t *testing.T) {
	h := newHarness(t,
		scriptStep{err: errors.New("summarizer down")}, // compaction call fails
		scriptStep{resp: textResp("done")},
	)
	seedOldTurns(h, 4)
	h.deps.Limits = Limits{ContextWindowTokens: 8000, KeepRecentTurns: 2}
	h.mustRun()

	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q: compaction is an optimization and must not fail the turn", fin.Status)
	}
	if !strings.Contains(requestText(h.prov.reqs[1]), "USER-old-1") {
		t.Error("without a summary the full history must still be sent")
	}
}
