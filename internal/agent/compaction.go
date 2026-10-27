package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// Context compaction (AFB-10): when the estimated prompt exceeds a fraction of
// the model's window, older turns are replaced IN THE PROMPT (never in the DB)
// by a summary produced with one extra model call. The summary is stored as a
// `summary` message whose metadata names the last message it replaces
// (summaryMeta), so the next turns reuse it without another model call and only
// re-summarize when the prompt grows past the threshold again.

const (
	charsPerToken = 4

	summaryToolResultChars = 300
	summaryAssistantChars  = 1500
	summaryTranscriptChars = 40000
	summaryMaxOutputTokens = 1200

	summaryPreamble = "Summary of the earlier conversation (older turns were compacted to save context):\n"

	summarySystemPrompt = `You compress the earlier part of a conversation between a user and a coding agent that builds a React web app. ` +
		`Write a faithful, compact summary the agent can rely on instead of the original messages. Keep: what the user asked for and any ` +
		`preferences or constraints, the app structure and files created or changed, decisions taken, the backend schema facts that were ` +
		`established, and anything unfinished or failing. Do not invent details. Plain text, at most 400 words.`
)

// splitSummary returns the newest summary message (nil when none) and the
// messages that follow it, summary rows excluded.
func (r *run) splitSummary() (*store.Message, []store.Message) {
	var summary *store.Message
	var through int64
	for i := range r.msgs {
		m := &r.msgs[i]
		if m.Role != store.RoleSummary {
			continue
		}
		meta, ok := parseSummaryMeta(*m)
		if !ok {
			continue
		}
		if summary == nil || meta.ThroughMessageID >= through {
			summary, through = m, meta.ThroughMessageID
		}
	}
	base := make([]store.Message, 0, len(r.msgs))
	for _, m := range r.msgs {
		if m.Role == store.RoleSummary || m.ID <= through {
			continue
		}
		base = append(base, m)
	}
	return summary, base
}

// promptMessages builds the conversation for the next model call, compacting
// first when the estimate is over the threshold (or when force is set because
// the provider rejected the prompt as too long).
func (r *run) promptMessages(ctx, tctx context.Context, force bool) ([]provider.Message, error) {
	summary, base := r.splitSummary()

	budgetChars := int(float64(r.lim.ContextWindowTokens*charsPerToken) * r.lim.CompactionThreshold)
	if !force && r.estimateChars(summary, base) <= budgetChars {
		return assemble(summary, base, r.turn.ID), nil
	}

	older, keep := splitOlderTurns(base, r.lim.KeepRecentTurns)
	if len(older) == 0 {
		return assemble(summary, base, r.turn.ID), nil // nothing old enough to compact
	}

	text, err := r.summarize(ctx, tctx, summary, older)
	if err != nil {
		if force || errors.Is(err, store.ErrLeaseLost) || ctx.Err() != nil {
			return nil, err
		}
		// Compaction is an optimization: keep going with the full history.
		r.log.Error("agent compaction failed", "turn", r.turn.ID, "session", r.sess.ID, "err", err)
		return assemble(summary, base, r.turn.ID), nil
	}

	meta, _ := json.Marshal(summaryMeta{ThroughMessageID: older[len(older)-1].ID})
	m, err := r.deps.Store.AppendMessage(ctx, r.lease, store.NewMessage{Role: store.RoleSummary, Content: text, ToolCalls: meta})
	if err != nil {
		return nil, fmt.Errorf("persist summary: %w", err)
	}
	r.msgs = append(r.msgs, *m)
	return assemble(m, keep, r.turn.ID), nil
}

func assemble(summary *store.Message, base []store.Message, turnID string) []provider.Message {
	out := toProviderMessages(base, turnID)
	if summary == nil {
		return out
	}
	head := provider.Message{Role: provider.RoleUser, Content: summaryPreamble + summary.Content}
	return append([]provider.Message{head}, out...)
}

// estimateChars is the chars/4 heuristic input: system prompt, tool specs and
// every message that would be sent.
func (r *run) estimateChars(summary *store.Message, base []store.Message) int {
	n := len(r.system)
	for _, t := range r.deps.Tools.Specs() {
		n += len(t.Name) + len(t.Description) + len(t.Schema)
	}
	if summary != nil {
		n += len(summaryPreamble) + len(summary.Content)
	}
	for _, m := range base {
		n += len(m.Content) + len(m.ToolCalls) + len(m.ToolCallID)
	}
	return n
}

// splitOlderTurns separates base into the turns to summarize and the last
// keepTurns turns to keep verbatim. A turn is a run of messages with the same
// turn id.
func splitOlderTurns(base []store.Message, keepTurns int) (older, keep []store.Message) {
	var starts []int
	for i, m := range base {
		if i == 0 || m.TurnID != base[i-1].TurnID {
			starts = append(starts, i)
		}
	}
	if len(starts) <= keepTurns {
		return nil, base
	}
	cut := starts[len(starts)-keepTurns]
	return base[:cut], base[cut:]
}

// summarize runs the one extra model call. It is subject to the same budget
// check as any other call, and its usage is recorded.
func (r *run) summarize(ctx, tctx context.Context, prev *store.Message, older []store.Message) (string, error) {
	if err := r.deps.Budget.Check(tctx, r.sess.OwnerUserID, r.sess.ID); err != nil {
		return "", fmt.Errorf("budget check before compaction: %w", err)
	}
	resp, err := r.deps.Provider.Chat(tctx, provider.ChatRequest{
		System:          summarySystemPrompt,
		Messages:        []provider.Message{{Role: provider.RoleUser, Content: renderTranscript(prev, older)}},
		MaxOutputTokens: summaryMaxOutputTokens,
	})
	if err != nil {
		return "", fmt.Errorf("summarize: %w", err)
	}
	if err := r.deps.Store.RecordUsage(ctx, store.UsageRecord{
		UserID: r.sess.OwnerUserID, Feature: store.FeatureBuilder, SessionID: r.sess.ID, TurnID: r.turn.ID,
		Provider: r.deps.Provider.Name(), Model: r.deps.Provider.Model(),
		InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens,
	}); err != nil {
		return "", fmt.Errorf("record compaction usage: %w", err)
	}
	text := strings.TrimSpace(resp.Message.Content)
	if text == "" {
		return "", errors.New("summarize: empty summary")
	}
	return text, nil
}

// renderTranscript is the compact text the summarizer reads. Tool results and
// assistant text are cut so the compaction call itself stays small; file
// contents sent by write_file are never included, only the tool name and path.
func renderTranscript(prev *store.Message, older []store.Message) string {
	var b strings.Builder
	if prev != nil {
		b.WriteString("Previous summary:\n" + prev.Content + "\n\n")
	}
	b.WriteString("Conversation to summarize:\n")
	for _, m := range older {
		switch m.Role {
		case store.RoleUser:
			fmt.Fprintf(&b, "User: %s\n", capRunes(m.Content, summaryAssistantChars))
		case store.RoleAssistant:
			if strings.TrimSpace(m.Content) != "" {
				fmt.Fprintf(&b, "Assistant: %s\n", capRunes(m.Content, summaryAssistantChars))
			}
			for _, c := range parseAssistantCalls(m) {
				fmt.Fprintf(&b, "Assistant called %s %s\n", c.Name, toolSummary(c.Arguments))
			}
		case store.RoleTool:
			fmt.Fprintf(&b, "Tool result: %s\n", capRunes(m.Content, summaryToolResultChars))
		}
	}
	s := b.String()
	if len(s) > summaryTranscriptChars {
		s = "[earlier part omitted]\n" + tailRunes(s, summaryTranscriptChars)
	}
	return s
}
