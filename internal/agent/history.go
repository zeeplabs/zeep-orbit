package agent

import (
	"context"
	"encoding/json"

	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// ---- message metadata ----
//
// builder_messages has no metadata column, so the loop keeps two small facts
// in the existing tool_calls JSONB column of non-assistant rows (assistant rows
// keep the provider tool calls array there):
//
//   - tool rows:    {"build_failed": bool, "is_error": bool, "skill": name},
//     so a resumed turn can recount failed builds (fix iterations) and the
//     distinct skills it loaded from the DB alone;
//   - summary rows: {"through_message_id": N}, the last message the summary
//     replaces (context compaction).
type toolMeta struct {
	BuildFailed bool `json:"build_failed"`
	IsError     bool `json:"is_error"`
	// Skill is the skill a successful load_skill call loaded (empty otherwise).
	Skill string `json:"skill,omitempty"`
}

type summaryMeta struct {
	ThroughMessageID int64 `json:"through_message_id"`
}

func parseToolMeta(m store.Message) toolMeta {
	var t toolMeta
	if len(m.ToolCalls) > 0 {
		_ = json.Unmarshal(m.ToolCalls, &t)
	}
	return t
}

func parseSummaryMeta(m store.Message) (summaryMeta, bool) {
	var s summaryMeta
	if len(m.ToolCalls) == 0 || json.Unmarshal(m.ToolCalls, &s) != nil || s.ThroughMessageID <= 0 {
		return summaryMeta{}, false
	}
	return s, true
}

func parseAssistantCalls(m store.Message) []provider.ToolCall {
	if len(m.ToolCalls) == 0 {
		return nil
	}
	var calls []provider.ToolCall
	if err := json.Unmarshal(m.ToolCalls, &calls); err != nil {
		return nil
	}
	return calls
}

// ---- turn state rebuilt from the DB ----

// turnState is what a (re)started turn needs to know about itself.
type turnState struct {
	// steps is the number of model calls already made in this turn.
	steps int
	// failedBuilds is the number of failed run_build calls so far.
	failedBuilds int
	// skills are the distinct skills loaded so far in this turn.
	skills map[string]bool
	// started is true when the turn already produced any assistant output, so
	// this invocation is a resume.
	started bool
	// pending are the tool calls of the last assistant message that have no
	// result yet, in call order (empty when every call was answered).
	pending []provider.ToolCall
	// lastAssistantNoTools is true when the last message of the turn is an
	// assistant reply without tool calls (the model was done; only the final
	// commit is missing).
	lastAssistantNoTools bool
}

func analyzeTurn(msgs []store.Message, turnID string) turnState {
	var st turnState
	answered := map[string]bool{}
	var lastAssistant *store.Message
	for i := range msgs {
		m := &msgs[i]
		if m.TurnID != turnID {
			continue
		}
		switch m.Role {
		case store.RoleAssistant:
			st.steps++
			st.started = true
			lastAssistant = m
		case store.RoleTool:
			answered[m.ToolCallID] = true
			meta := parseToolMeta(*m)
			if meta.BuildFailed {
				st.failedBuilds++
			}
			if meta.Skill != "" {
				if st.skills == nil {
					st.skills = map[string]bool{}
				}
				st.skills[meta.Skill] = true
			}
		}
	}
	if lastAssistant == nil {
		return st
	}
	calls := parseAssistantCalls(*lastAssistant)
	if len(calls) == 0 {
		st.lastAssistantNoTools = true
		return st
	}
	for _, c := range calls {
		if !answered[c.ID] {
			st.pending = append(st.pending, c)
		}
	}
	return st
}

// ---- DB -> provider messages ----

// toProviderMessages converts persisted messages into the conversation sent to
// the model. Summary rows are skipped (compaction handles them). For turns
// other than currentTurnID, an assistant tool call that never got a result
// (the turn ended mid-batch) gets a synthetic result, because providers reject
// a tool call without an answer; the current turn executes such calls instead.
func toProviderMessages(msgs []store.Message, currentTurnID string) []provider.Message {
	answered := map[string]bool{}
	for _, m := range msgs {
		if m.Role == store.RoleTool {
			answered[m.ToolCallID] = true
		}
	}
	out := make([]provider.Message, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case store.RoleUser:
			out = append(out, provider.Message{Role: provider.RoleUser, Content: m.Content})
		case store.RoleAssistant:
			calls := parseAssistantCalls(m)
			out = append(out, provider.Message{Role: provider.RoleAssistant, Content: m.Content, ToolCalls: calls})
			if m.TurnID != currentTurnID {
				for _, c := range calls {
					if !answered[c.ID] {
						out = append(out, provider.Message{
							Role: provider.RoleTool, ToolCallID: c.ID,
							Content: "this tool call was not executed because the turn ended before it ran",
						})
					}
				}
			}
		case store.RoleTool:
			out = append(out, provider.Message{Role: provider.RoleTool, Content: m.Content, ToolCallID: m.ToolCallID})
		}
	}
	return out
}

// ---- snapshots ----

// BundlesFromSnapshots maps stored snapshots, already in (turn_seq, part)
// order as ListSnapshots returns them, to the ordered bundle list
// [sandbox.Sandbox.Restore] expects: one bundle per row.
func BundlesFromSnapshots(snaps []store.Snapshot) []sandbox.Bundle {
	out := make([]sandbox.Bundle, 0, len(snaps))
	for _, sn := range snaps {
		out = append(out, sandbox.Bundle{Seq: sn.TurnSeq, SHA: sn.CommitSHA, Data: sn.Bundle})
	}
	return out
}

// SnapshotLister is the part of the store [SnapshotProvider] needs.
type SnapshotLister interface {
	ListSnapshots(ctx context.Context, sessionID string) ([]store.Snapshot, error)
}

// SnapshotProvider adapts the store to [sandbox.SnapshotProvider], so the
// restoring sandbox (sandbox.NewRestoring) can rebuild a lost workspace.
func SnapshotProvider(l SnapshotLister) sandbox.SnapshotProvider {
	return func(ctx context.Context, sessionID string) ([]sandbox.Bundle, error) {
		snaps, err := l.ListSnapshots(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		return BundlesFromSnapshots(snaps), nil
	}
}
