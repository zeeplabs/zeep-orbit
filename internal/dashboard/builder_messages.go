package dashboard

// builder_messages.go — chat endpoints of the AI Frontend Builder (T26, AFB-05
// and AFB-06):
//
//	POST /api/builder/sessions/{id}/messages   h.CreateMessage   enqueue a turn
//	GET  /api/builder/sessions/{id}/events     h.ListEvents      cursor polling
//
// Both are owner-only (ownedSession: anyone else gets a 404). Event payloads
// are re-filtered on the way out (sanitizeBuilderEventPayload), so what the
// loop wrote to the database is never trusted to be safe to serve.

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// Limits of the chat endpoints.
const (
	// builderMessageBodyLimit caps the request body; a message is at most
	// builderMaxMessageRunes runes, so this leaves room for JSON escaping.
	builderMessageBodyLimit = 64 << 10
	// builderMaxMessageRunes caps one user message (it is replayed to the model
	// on every later step of the session).
	builderMaxMessageRunes = 8000

	// Page size of GET .../events: builderEventsDefaultLimit when `limit` is
	// absent, never more than builderEventsMaxLimit.
	builderEventsDefaultLimit = store.DefaultEventsLimit
	builderEventsMaxLimit     = store.MaxEventsLimit
)

// Public messages of the chat endpoints.
const (
	builderTurnActiveMsg = "a turn is already in progress"
)

type createBuilderMessageRequest struct {
	Content string `json:"content"`
}

// CreateMessage handles POST /dashboard/api/builder/sessions/{id}/messages:
// it records the user's message and enqueues a pending chat turn, answering
// 202 with the turn. A session that already has a non-terminal turn (running,
// queued, or paused on a proposal) answers 409: one turn at a time. A failed
// session still accepts messages; that is how the user retries.
func (h *BuilderHandler) CreateMessage(w http.ResponseWriter, r *http.Request) {
	user, sess, ok := h.ownedSession(w, r, false)
	if !ok {
		return
	}
	var req createBuilderMessageRequest
	if !h.decodeBuilderBody(w, r, &req, builderMessageBodyLimit) {
		return
	}
	content := strings.TrimSpace(req.Content)
	switch {
	case content == "":
		h.writeError(w, http.StatusBadRequest, "message is required")
		return
	case !utf8.ValidString(content) || strings.ContainsRune(content, 0):
		h.writeError(w, http.StatusBadRequest, "message contains invalid characters")
		return
	case utf8.RuneCountInString(content) > builderMaxMessageRunes:
		h.writeError(w, http.StatusBadRequest, "message is too long")
		return
	}
	if _, ok := h.requireAvailable(w, r); !ok {
		return
	}

	turn, err := h.store.CreateTurn(r.Context(), user.ID, sess.ID, store.KindChat, content)
	if !h.turnCreated(w, r, turn, err) {
		return
	}
	// Chat content is never audited: the row records that a turn was requested.
	h.audit(r.Context(), user, "builder.message.create", sess.ID, sess.Title,
		map[string]any{"turn_seq": turn.Seq}, r.RemoteAddr)
}

// turnCreated finishes a create-turn request shared by messages and reverts:
// it maps the store errors, pokes the worker and answers 202 with the turn. It
// reports whether the turn was created.
func (h *BuilderHandler) turnCreated(w http.ResponseWriter, r *http.Request, turn *store.Turn, err error) bool {
	switch {
	case err == nil:
	case errors.Is(err, store.ErrTurnActive):
		h.writeError(w, http.StatusConflict, builderTurnActiveMsg)
		return false
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrSessionArchived):
		h.writeError(w, http.StatusNotFound, builderSessionNotFoundMsg)
		return false
	default:
		h.serverError(w, r, err)
		return false
	}
	if h.poke != nil {
		h.poke()
	}
	writeJSON(w, http.StatusAccepted, newBuilderTurnView(turn))
	return true
}

// BuilderEventView is one entry of the event log. Payload is whitelisted per
// event type; it never carries file contents or raw provider/builder errors.
type BuilderEventView struct {
	ID        int64           `json:"id"`
	TurnID    *string         `json:"turn_id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// BuilderEventsPage is the body of GET .../events. Pass NextCursor as `after`
// to continue; it equals the request's `after` when there is nothing new.
type BuilderEventsPage struct {
	Events     []BuilderEventView `json:"events"`
	NextCursor int64              `json:"next_cursor"`
}

// ListEvents handles GET /dashboard/api/builder/sessions/{id}/events?after=&limit=:
// events with id > after (default 0), oldest first, at most limit (default
// builderEventsDefaultLimit, capped at builderEventsMaxLimit). A malformed
// after/limit is a 400.
func (h *BuilderHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	user, sess, ok := h.ownedSession(w, r, false)
	if !ok {
		return
	}
	q := r.URL.Query()
	var after int64
	if v := q.Get("after"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			h.writeError(w, http.StatusBadRequest, "invalid after cursor")
			return
		}
		after = n
	}
	limit := builderEventsDefaultLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			h.writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = min(n, builderEventsMaxLimit)
	}

	events, err := h.store.ListEvents(r.Context(), user.ID, sess.ID, after, limit)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, builderSessionNotFoundMsg)
			return
		}
		h.serverError(w, r, err)
		return
	}
	page := BuilderEventsPage{Events: make([]BuilderEventView, 0, len(events)), NextCursor: after}
	for i := range events {
		e := &events[i]
		page.Events = append(page.Events, BuilderEventView{
			ID: e.ID, TurnID: e.TurnID, Type: e.Type, CreatedAt: e.CreatedAt,
			Payload: sanitizeBuilderEventPayload(e.Type, e.Payload),
		})
		page.NextCursor = e.ID
	}
	writeJSON(w, http.StatusOK, page)
}

// ---- event payload whitelist ----

type eventFieldKind int

const (
	efString eventFieldKind = iota
	efInt
	efBool
	efObject // an arbitrary JSON object, size-capped
)

type eventField struct {
	kind eventFieldKind
	max  int // runes for strings, bytes for objects
}

const (
	maxEventObjectBytes = 8 << 10
	maxEventShortString = 256
	maxEventTextString  = 8000
	maxEventTailString  = 2000
)

func efStr(n int) eventField { return eventField{kind: efString, max: n} }

var (
	efNum  = eventField{kind: efInt}
	efFlag = eventField{kind: efBool}
	efObj  = eventField{kind: efObject, max: maxEventObjectBytes}
)

// builderEventFields lists, per event type, the only payload keys that leave
// the server. Everything else the loop might have stored is dropped: file
// contents, tool arguments, provider or builder error text.
var builderEventFields = map[string]map[string]eventField{
	"turn_started":      {"seq": efNum, "resumed": efFlag, "kind": efStr(32)},
	"queued":            {},
	"assistant_message": {"content": efStr(maxEventTextString)},
	"tool_call":         {"name": efStr(64), "summary": efStr(maxEventShortString)},
	"file_changed":      {"path": efStr(maxEventShortString), "op": efStr(16)},
	"build_started":     {"step": efStr(16)},
	"build_succeeded":   {"step": efStr(16)},
	"build_failed":      {"step": efStr(16), "tail": efStr(maxEventTailString)},
	"proposal":          {"tool_call_id": efStr(128), "name": efStr(64), "operation": efObj},
	"limit_reached":     {"limit": efStr(32)},
	"budget_exceeded":   {},
	"error":             {"code": efStr(64)},
	"revert":            {"to_turn_seq": efNum, "sha": efStr(64), "changed": efFlag, "build": efStr(16)},
	"turn_finished":     {"status": efStr(32), "sha": efStr(64)},
}

var (
	eventKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	// eventDeniedKeys are never served for event types without a whitelist:
	// names under which free text, file contents or raw errors travel.
	eventDeniedKeys = map[string]bool{
		"content": true, "tail": true, "error": true, "err": true, "message": true, "detail": true,
		"details": true, "body": true, "output": true, "text": true, "stack": true, "trace": true,
		"file": true, "files": true, "diff": true, "arguments": true, "args": true, "result": true,
	}
)

// sanitizeBuilderEventPayload returns the part of an event payload that may be
// served. Known event types keep only their whitelisted, correctly typed keys.
// Other types (publish_*, proposal_confirmed, ... written by other handlers)
// keep only short scalar values under keys that are not on the deny list, so a
// new event type can never leak text or objects by accident; add it to
// builderEventFields to serve more.
func sanitizeBuilderEventPayload(eventType string, raw json.RawMessage) json.RawMessage {
	empty := json.RawMessage(`{}`)
	var in map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &in) != nil {
		return empty
	}
	out := map[string]any{}
	if fields, known := builderEventFields[eventType]; known {
		for key, f := range fields {
			if v, ok := in[key]; ok {
				if clean, ok := cleanEventValue(v, f); ok {
					out[key] = clean
				}
			}
		}
	} else {
		for key, v := range in {
			if !eventKeyPattern.MatchString(key) || eventDeniedKeys[key] {
				continue
			}
			var scalar any
			if json.Unmarshal(v, &scalar) != nil {
				continue
			}
			switch s := scalar.(type) {
			case string:
				out[key] = capEventString(s, maxEventShortString)
			case float64, bool:
				out[key] = s
			}
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return empty
	}
	return b
}

func cleanEventValue(v json.RawMessage, f eventField) (any, bool) {
	switch f.kind {
	case efString:
		var s string
		if json.Unmarshal(v, &s) != nil {
			return nil, false
		}
		return capEventString(s, f.max), true
	case efInt:
		var n int64
		if json.Unmarshal(v, &n) != nil {
			return nil, false
		}
		return n, true
	case efBool:
		var b bool
		if json.Unmarshal(v, &b) != nil {
			return nil, false
		}
		return b, true
	case efObject:
		var m map[string]json.RawMessage
		if len(v) > f.max || json.Unmarshal(v, &m) != nil || m == nil {
			return nil, false
		}
		return json.RawMessage(v), true
	}
	return nil, false
}

func capEventString(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
