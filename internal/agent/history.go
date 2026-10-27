package agent

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

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
//   - tool rows:    {"build_failed": bool, "is_error": bool}, so a resumed
//     turn can recount failed builds (fix iterations) from the DB alone;
//   - summary rows: {"through_message_id": N}, the last message the summary
//     replaces (context compaction).
type toolMeta struct {
	BuildFailed bool `json:"build_failed"`
	IsError     bool `json:"is_error"`
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
			if parseToolMeta(*m).BuildFailed {
				st.failedBuilds++
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

// ---- snapshot framing ----
//
// builder_snapshots keeps ONE row per (session, turn_seq), but the loop
// commits after every tool batch that wrote files, so a turn can produce
// several commits and each one has its own incremental bundle (prev..sha).
// Overwriting the row would drop the earlier bundles and break the restore
// chain. The loop therefore stores the bundles of a turn in that single row,
// framed as:
//
//	magic | (uvarint len(sha) | sha | uvarint len(bundle) | bundle)*
//
// Rows without the magic (the turn-0 snapshot written at session creation) are
// a plain git bundle whose commit is the row's commit_sha.
const snapshotMagic = "ORBSNAP1\n"

type snapPart struct {
	SHA  string
	Data []byte
}

func encodeSnapshotParts(parts []snapPart) []byte {
	var b bytes.Buffer
	b.WriteString(snapshotMagic)
	var tmp [binary.MaxVarintLen64]byte
	for _, p := range parts {
		n := binary.PutUvarint(tmp[:], uint64(len(p.SHA)))
		b.Write(tmp[:n])
		b.WriteString(p.SHA)
		n = binary.PutUvarint(tmp[:], uint64(len(p.Data)))
		b.Write(tmp[:n])
		b.Write(p.Data)
	}
	return b.Bytes()
}

func decodeSnapshotParts(sn store.Snapshot) ([]snapPart, error) {
	if !bytes.HasPrefix(sn.Bundle, []byte(snapshotMagic)) {
		return []snapPart{{SHA: sn.CommitSHA, Data: sn.Bundle}}, nil
	}
	r := bytes.NewReader(sn.Bundle[len(snapshotMagic):])
	var parts []snapPart
	read := func() ([]byte, error) {
		n, err := binary.ReadUvarint(r)
		if err != nil || n > uint64(r.Len()) {
			return nil, errors.New("agent: corrupt snapshot frame")
		}
		buf := make([]byte, n)
		if _, err := r.Read(buf); err != nil && n > 0 {
			return nil, errors.New("agent: corrupt snapshot frame")
		}
		return buf, nil
	}
	for r.Len() > 0 {
		sha, err := read()
		if err != nil {
			return nil, err
		}
		data, err := read()
		if err != nil {
			return nil, err
		}
		parts = append(parts, snapPart{SHA: string(sha), Data: data})
	}
	return parts, nil
}

// BundlesFromSnapshots flattens stored snapshots (turn_seq order) into the
// ordered bundle list [sandbox.Sandbox.Restore] expects, unpacking the framed
// multi-commit rows the loop writes.
func BundlesFromSnapshots(snaps []store.Snapshot) ([]sandbox.Bundle, error) {
	var out []sandbox.Bundle
	for _, sn := range snaps {
		parts, err := decodeSnapshotParts(sn)
		if err != nil {
			return nil, fmt.Errorf("snapshot of turn %d: %w", sn.TurnSeq, err)
		}
		for _, p := range parts {
			out = append(out, sandbox.Bundle{Seq: sn.TurnSeq, SHA: p.SHA, Data: p.Data})
		}
	}
	return out, nil
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
		return BundlesFromSnapshots(snaps)
	}
}
