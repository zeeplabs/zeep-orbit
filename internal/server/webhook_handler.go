package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/query"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
	"github.com/zeeplabs/zeep-orbit/internal/webhookengine"
)

// WebhookHandler serves the public, unauthenticated (token-in-URL) inbound
// webhook route — no dashboard session, no end-user JWT. See design.md
// Architecture Overview and Components > Public Webhook Handler.
type WebhookHandler struct {
	pool *db.Pool
	reg  *registry.Registry

	// limiter is optional (nil in tests): set via SetRateLimiter once the
	// handler is wired into the router. Kept separate from the constructor
	// instead of a parameter so the ~30 existing test call sites don't need
	// touching — a nil limiter just skips the check (see HandleWebhookDelivery).
	// Charged only once a request's token has verified — see
	// HandleWebhookDelivery — so a real subscription's budget can only be
	// spent by genuine deliveries, never by someone guessing tokens against a
	// known webhookId (visible in the dashboard URL).
	limiter *dashboard.RateLimiter

	// lookupLimiter bounds GetWebhookByID lookups before a webhook row (and
	// therefore any per-webhook budget) exists to charge against — keyed by
	// source IP, coarse and shared across tenants behind the LB on purpose
	// (see SetLookupRateLimiter): its job is bounding raw junk traffic to the
	// database, not per-tenant fairness, which the resolved-id limiter above
	// already provides once a request clears this gate.
	lookupLimiter *dashboard.RateLimiter

	// authFailureLimiter bounds invalid-token attempts against a real,
	// resolved webhookId — keyed by wh.ID, separate from limiter so a flood
	// of garbage tokens against a known id can't exhaust the budget real
	// deliveries need (see SetAuthFailureRateLimiter).
	authFailureLimiter *dashboard.RateLimiter

	// dedupLockPool is a small pool dedicated to lockEventID's held
	// connections, separate from pool so a burst of concurrent deliveries
	// with distinct event ids can't tie up every connection the actual
	// dedup-check/write/log queries need — see lockEventID and
	// dedupLockPoolMaxConns. Created lazily (first delivery that carries an
	// event id) since NewWebhookHandler isn't given a context to dial with.
	dedupLockOnce sync.Once
	dedupLockPool *db.Pool
	dedupLockErr  error
}

// dedupLockPoolMaxConns caps the dedup lock pool well below the main pool's
// capacity (internal/db/client.go, currently 10) so lock-holding deliveries
// can never starve the connections the main pool's writes/reads need to
// finish (and thereby release the lock) — see lockEventID.
const dedupLockPoolMaxConns = 4

// NewWebhookHandler creates a WebhookHandler with injected dependencies.
func NewWebhookHandler(pool *db.Pool, reg *registry.Registry) *WebhookHandler {
	return &WebhookHandler{pool: pool, reg: reg}
}

// SetRateLimiter wires a rate limiter that HandleWebhookDelivery consults
// after resolving {webhookId} against the database, keyed by the resolved
// wh.ID rather than the raw URL param. Doing the resolution first means a
// made-up or soft-deleted id never gets a fresh rate-limit budget of its
// own — previously the route was wrapped in
// limiter.MiddlewareKeyedBy(webhookId-from-URL), which charged budget
// against whatever string the caller put in the URL, existing or not
// (D-175: "rate limiter accepts a new budget for a nonexistent webhookId").
func (h *WebhookHandler) SetRateLimiter(rl *dashboard.RateLimiter) {
	h.limiter = rl
}

// SetLookupRateLimiter wires a rate limiter that HandleWebhookDelivery
// consults before resolving {webhookId} against the database — keyed by
// source IP rather than webhookId, since at this point no webhook row (and
// therefore no per-tenant budget) has been resolved yet. Without this, a
// flood of requests against a single fixed nonexistent id performs one
// unbounded GetWebhookByID per request forever: SetRateLimiter's budget
// never applies to an id that never resolves.
func (h *WebhookHandler) SetLookupRateLimiter(rl *dashboard.RateLimiter) {
	h.lookupLimiter = rl
}

// SetAuthFailureRateLimiter wires a rate limiter that HandleWebhookDelivery
// consults on an invalid-token attempt against a resolved webhookId — kept
// separate from SetRateLimiter's budget so a flood of garbage tokens against
// a real, known webhookId (visible in the dashboard URL) can't exhaust the
// budget genuine deliveries need.
func (h *WebhookHandler) SetAuthFailureRateLimiter(rl *dashboard.RateLimiter) {
	h.authFailureLimiter = rl
}

// HandleWebhookDelivery is the single entrypoint for
// /hooks/{webhookId}/{token}, registered method-agnostically — the handler
// itself checks the stored method against r.Method and 404s on mismatch,
// per WEBHOOK-04, before ever touching the token.
// maxWebhookBodyBytes caps an inbound webhook request body — this route is
// public and unauthenticated until the token check below, so an unbounded
// io.ReadAll in parseWebhookPayload would let anyone who knows a webhookId
// (visible in the dashboard URL) submit an arbitrarily large body.
const maxWebhookBodyBytes = 1 << 20 // 1 MiB

func (h *WebhookHandler) HandleWebhookDelivery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)
	}

	ctx := r.Context()
	webhookID := chi.URLParam(r, "webhookId")
	token := chi.URLParam(r, "token")

	// Coarse, pre-resolution guard: no webhook row exists yet at this point,
	// so there's no per-tenant budget to key on — see SetLookupRateLimiter.
	if h.lookupLimiter != nil && !h.lookupLimiter.Allow(remoteIP(r)) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "too many requests")
		return
	}

	// Unscoped lookup: the public URL carries only {webhookId}, not the
	// owning app. A soft-deleted or nonexistent webhook resolves to
	// ErrWebhookNotFound identically — both cases plain 404, no delivery
	// logged (design.md Error Handling Strategy: "call to a deleted
	// webhook's URL → 404; no delivery logged" and "method mismatch → 404,
	// not logged" — a nonexistent id is the same shape of non-event).
	wh, err := dashboard.GetWebhookByID(ctx, h.pool, "", webhookID)
	if err != nil {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	if r.Method != wh.Method {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	if !dashboard.VerifyWebhookToken(wh.TokenSecret, token) {
		// Own budget, not the delivery limiter below — a flood of garbage
		// tokens against this real, resolved id must not be able to exhaust
		// the quota genuine deliveries need. See SetAuthFailureRateLimiter.
		if h.authFailureLimiter != nil && !h.authFailureLimiter.Allow(wh.ID) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "too many requests")
			return
		}
		h.logDelivery(ctx, wh.ID, http.StatusUnauthorized, "invalid_token", nil, "", "", "")
		writeError(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}

	// Charged only for a request whose token just verified — keyed by the
	// resolved wh.ID, not the raw URL param. See SetRateLimiter.
	if h.limiter != nil && !h.limiter.Allow(wh.ID) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "too many requests")
		return
	}

	payload, err := parseWebhookPayload(r)
	if err != nil {
		h.logDelivery(ctx, wh.ID, http.StatusBadRequest, "malformed", nil, "", "", err.Error())
		writeError(w, http.StatusBadRequest, "malformed request body")
		return
	}

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		// Marshaling a map built entirely from decoded JSON / query params
		// back to JSON cannot realistically fail — defensive only.
		h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", nil, "", "", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Provider verification handshake (Slack Events API, and others that
	// follow the same convention): a subscription-time POST carrying a
	// top-level "challenge" string must be echoed back verbatim as
	// {"challenge": "..."} — not treated as a real event, so it bypasses
	// capture/mapping entirely regardless of the webhook's current status.
	if challenge, ok := verificationChallenge(payload); ok {
		// Store only the challenge value, not the full rawPayload -- some
		// providers following this convention (Slack's legacy Events API
		// shape) include a separate verification "token" field alongside
		// "challenge", which has no reason to sit in webhook_deliveries for
		// 30 days.
		minimal, _ := json.Marshal(map[string]string{"challenge": challenge})
		h.logDelivery(ctx, wh.ID, http.StatusOK, "verification_challenge", minimal, "", "", "")
		writeJSON(w, http.StatusOK, map[string]string{"challenge": challenge})
		return
	}

	if wh.Status == "capture" {
		if err := dashboard.StoreCapturedSample(ctx, h.pool, wh.ID, rawPayload); err != nil {
			h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", rawPayload, "", "", err.Error())
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		h.logDelivery(ctx, wh.ID, http.StatusOK, "captured", rawPayload, "", "", "")
		writeJSON(w, http.StatusOK, map[string]string{"status": "captured"})
		return
	}

	h.handleActiveDelivery(ctx, w, wh, payload, rawPayload)
}

// verificationChallenge reports whether payload carries a non-empty
// top-level "challenge" string — the field name used by Slack's Events API
// (and other providers following the same convention) for the one-time URL
// verification handshake sent when a webhook URL is first subscribed.
func verificationChallenge(payload map[string]any) (string, bool) {
	v, ok := payload["challenge"]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

// stringifyPathValue renders a resolved payload value (any JSON scalar) as
// a plain string for event-type/event-id comparison and dedup lookups —
// providers send these as either JSON strings or, occasionally, numbers.
func stringifyPathValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// resolveAppName looks up the owning app's name (the registry's lookup key)
// from a webhook's app_id — the public URL only carries {webhookId}, so the
// app name isn't known until the webhook row itself is resolved.
func resolveAppName(ctx context.Context, pool *db.Pool, appID string) (string, error) {
	var name string
	if err := pool.QueryRow(ctx, `SELECT name FROM zeep_system.apps WHERE id = $1`, appID).Scan(&name); err != nil {
		return "", err
	}
	return name, nil
}

// handleActiveDelivery dispatches an active-mode call: resolves the
// event-type value, looks up its saved mapping (unmapped → 200/logged,
// spec P2 AC5), checks event-id dedup (duplicate → 200/logged, no write,
// spec P2 AC4), then applies the mapping's action. Insert dispatch lands
// here (T7); update/delete land in T8.
func (h *WebhookHandler) handleActiveDelivery(ctx context.Context, w http.ResponseWriter, wh dashboard.WebhookRow, payload map[string]any, rawPayload []byte) {
	eventTypeRaw, found := webhookengine.ExtractPath(payload, wh.EventTypePath)
	var eventTypeValue string
	if found {
		eventTypeValue = stringifyPathValue(eventTypeRaw)
	}

	mapping, err := dashboard.GetEventMappingByType(ctx, h.pool, wh.ID, eventTypeValue)
	if err != nil {
		if errors.Is(err, dashboard.ErrMappingNotFound) {
			h.logDelivery(ctx, wh.ID, http.StatusOK, "unmapped", rawPayload, eventTypeValue, "", "")
			writeJSON(w, http.StatusOK, map[string]string{"status": "unmapped"})
			return
		}
		h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", rawPayload, eventTypeValue, "", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var eventID string
	if wh.EventIDPath != nil && *wh.EventIDPath != "" {
		if v, found := webhookengine.ExtractPath(payload, *wh.EventIDPath); found {
			eventID = stringifyPathValue(v)
		}
	}
	if eventID != "" {
		// idx_webhook_deliveries_dedup isn't a unique index (write_error/
		// row_not_found deliveries must be allowed to share an event_id with
		// a later retry, see processedOutcomes), so the check below and the
		// eventual write are only safe from a check-then-act race if
		// something else serializes concurrent deliveries of the same event
		// id — a provider retrying while the first attempt is still in
		// flight is the normal at-least-once case this feature exists for,
		// made more likely by running multiple replicas. The advisory lock
		// closes that window: a second concurrent request blocks here until
		// the first has committed its write and delivery log row, then sees
		// seen=true and takes the duplicate_skipped path correctly.
		unlock, err := h.lockEventID(ctx, wh.ID, eventID)
		if err != nil {
			h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", rawPayload, eventTypeValue, eventID, err.Error())
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		defer unlock()

		seen, err := dashboard.HasProcessedEventID(ctx, h.pool, wh.ID, eventID)
		if err != nil {
			h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", rawPayload, eventTypeValue, eventID, err.Error())
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if seen {
			h.logDelivery(ctx, wh.ID, http.StatusOK, "duplicate_skipped", rawPayload, eventTypeValue, eventID, "")
			writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate_skipped"})
			return
		}
	}

	appName, err := resolveAppName(ctx, h.pool, wh.AppID)
	if err != nil {
		h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", rawPayload, eventTypeValue, eventID, err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	app, ok := h.reg.Get(appName)
	if !ok {
		h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", rawPayload, eventTypeValue, eventID, fmt.Sprintf("app %q not found in registry", appName))
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	table, ok := app.Tables[mapping.TargetTable]
	if !ok {
		h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", rawPayload, eventTypeValue, eventID, fmt.Sprintf("target table %q not found", mapping.TargetTable))
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	fieldMappings := make([]webhookengine.FieldMapping, len(mapping.FieldMappings))
	for i, fm := range mapping.FieldMappings {
		fieldMappings[i] = webhookengine.FieldMapping{SourcePath: fm.SourcePath, Column: fm.Column}
	}
	resolved, err := webhookengine.ResolveFields(payload, fieldMappings)
	if err != nil {
		h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", rawPayload, eventTypeValue, eventID, err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	switch mapping.Action {
	case "insert":
		h.dispatchInsert(ctx, w, wh, app, table, mapping, resolved, rawPayload, eventTypeValue, eventID)
	case "update", "delete":
		h.dispatchUpdateOrDelete(ctx, w, wh, app, table, mapping, resolved, rawPayload, eventTypeValue, eventID)
	default:
		// SaveEventMapping only ever persists insert/update/delete
		// (ErrInvalidAction otherwise) — unreachable in practice, defensive only.
		h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", rawPayload, eventTypeValue, eventID, fmt.Sprintf("unknown mapping action %q", mapping.Action))
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// errWebhookRowNotFound is the sentinel dispatchUpdateOrDelete's
// WithRLSContext closure returns when the match-key lookup finds no row —
// distinguished from a genuine write failure so the caller logs
// row_not_found (200) instead of write_error (500), per spec P2
// update/delete-with-match-key AC4.
var errWebhookRowNotFound = errors.New("server: webhook match key matched no row")

// errWebhookAmbiguousMatch is dispatchUpdateOrDelete's sentinel for when the
// match-key lookup finds more than one row — the match key isn't actually
// unique for this table, so applying update/delete to whichever row the
// query happened to return first would silently corrupt unrelated data.
var errWebhookAmbiguousMatch = errors.New("server: webhook match key matched more than one row")

// matchColumnCast returns the same ::uuid/::timestamptz cast
// query.BuildInsert/Update apply internally (via the shared query.PgCast) —
// needed because pgx's extended protocol doesn't auto-cast a text parameter
// into a uuid column for a bare WHERE col = $1.
func matchColumnCast(table *registry.Table, column string) string {
	for _, c := range table.Columns {
		if c.Name == column {
			return query.PgCast(c.Type)
		}
	}
	return ""
}

// dispatchUpdateOrDelete locates the target row by the mapping's match key
// (resolved from the same field-mapping set as the written columns) and
// applies query.BuildUpdate/BuildDelete — both statements inside the one
// transaction WithRLSContext opens, per design.md Risks & Concerns (a
// lookup-then-write race is bounded the same way HandleUpdate/HandleDelete
// already accept it, not a new class of risk this feature introduces).
func (h *WebhookHandler) dispatchUpdateOrDelete(ctx context.Context, w http.ResponseWriter, wh dashboard.WebhookRow, app *registry.App, table *registry.Table, mapping dashboard.EventMappingRow, resolved map[string]any, rawPayload []byte, eventTypeValue, eventID string) {
	if mapping.MatchKeyColumn == nil || *mapping.MatchKeyColumn == "" {
		// SaveEventMapping enforces a non-empty match key for update/delete
		// (ErrMatchKeyRequired) — unreachable in practice, defensive only.
		h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", rawPayload, eventTypeValue, eventID, "mapping has no match_key_column")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	matchColumn := *mapping.MatchKeyColumn
	matchValue, ok := resolved[matchColumn]
	if !ok {
		h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", rawPayload, eventTypeValue, eventID,
			fmt.Sprintf("match_key_column %q is not among this mapping's field_mappings, so its value can't be resolved from the payload", matchColumn))
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// LIMIT 2, not 1: the match key has no enforced uniqueness constraint, so
	// a second row means this key is ambiguous for this table — caught here
	// instead of silently applying the write to whichever row Postgres
	// happened to return first.
	//
	// Excludes soft-deleted rows when soft delete is on, matching
	// query.BuildDelete/BuildUpdate's own filter — without this, a
	// soft-deleted row sharing a match key value with a live one always
	// looks ambiguous (false-positive ambiguous_match), and on the update
	// path with zero live matches, the lookup would still find the deleted
	// row and resurrect its content instead of correctly reporting
	// row_not_found.
	deletedFilter := ""
	if h.reg.SystemConfig().SoftDeleteEnabled {
		deletedFilter = " AND deleted_at IS NULL"
	}
	lookupSQL := fmt.Sprintf(`SELECT id FROM %s.%s WHERE %s = $1%s%s LIMIT 2`,
		app.SchemaName, mapping.TargetTable, matchColumn, matchColumnCast(table, matchColumn), deletedFilter)

	var targetRowID string
	var outcome string
	err := h.pool.WithRLSContext(ctx, db.RLSClaims{Role: "webhook", Sub: wh.ID}, h.reg.SystemConfig().StatementTimeoutMs, func(qx db.Querier) error {
		rows, err := qx.Query(ctx, lookupSQL, matchValue)
		if err != nil {
			return err
		}
		matchedIDs, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		if len(matchedIDs) == 0 {
			return errWebhookRowNotFound
		}
		if len(matchedIDs) > 1 {
			return errWebhookAmbiguousMatch
		}
		rowID := matchedIDs[0]

		switch mapping.Action {
		case "update":
			q, err := query.BuildUpdate(app.SchemaName, mapping.TargetTable, table, rowID, resolved, "")
			if err != nil {
				return err
			}
			rows, err := qx.Query(ctx, q.SQL, q.Args...)
			if err != nil {
				return err
			}
			row, err := pgx.CollectOneRow(rows, pgx.RowToMap)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return errWebhookRowNotFound
				}
				return err
			}
			targetRowID, _ = sanitizeRow(row)["id"].(string)
			outcome = "updated"
		case "delete":
			q := query.BuildDelete(app.SchemaName, mapping.TargetTable, rowID, "", h.reg.SystemConfig().SoftDeleteEnabled)
			tag, err := qx.Exec(ctx, q.SQL, q.Args...)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return errWebhookRowNotFound
			}
			targetRowID = rowID
			outcome = "deleted"
		}
		return nil
	})

	if err != nil {
		if errors.Is(err, errWebhookRowNotFound) {
			h.logDelivery(ctx, wh.ID, http.StatusOK, "row_not_found", rawPayload, eventTypeValue, eventID, "")
			writeJSON(w, http.StatusOK, map[string]string{"status": "row_not_found"})
			return
		}
		if errors.Is(err, errWebhookAmbiguousMatch) {
			detail := fmt.Sprintf("match_key_column %q matched more than one row in %q — add a unique index or pick a column that's actually unique", matchColumn, mapping.TargetTable)
			h.logDelivery(ctx, wh.ID, http.StatusConflict, "ambiguous_match", rawPayload, eventTypeValue, eventID, detail)
			writeError(w, http.StatusConflict, "match key is ambiguous: matched more than one row")
			return
		}
		h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", rawPayload, eventTypeValue, eventID, err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.logDeliveryWithTarget(ctx, wh.ID, http.StatusOK, outcome, rawPayload, eventTypeValue, eventID, targetRowID, "")
	writeJSON(w, http.StatusOK, map[string]any{"status": outcome, "id": targetRowID})
}

// dispatchInsert runs query.BuildInsert inside WithRLSContext under the
// dedicated "webhook" RLS role (design.md Tech Decisions), exactly the
// pattern end-user write requests already use (internal/server/handler.go
// HandleCreate) — no bypass, no second authorization path.
func (h *WebhookHandler) dispatchInsert(ctx context.Context, w http.ResponseWriter, wh dashboard.WebhookRow, app *registry.App, table *registry.Table, mapping dashboard.EventMappingRow, resolved map[string]any, rawPayload []byte, eventTypeValue, eventID string) {
	q, err := query.BuildInsert(app.SchemaName, mapping.TargetTable, table, resolved, "")
	if err != nil {
		h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", rawPayload, eventTypeValue, eventID, err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var row map[string]any
	err = h.pool.WithRLSContext(ctx, db.RLSClaims{Role: "webhook", Sub: wh.ID}, h.reg.SystemConfig().StatementTimeoutMs, func(qx db.Querier) error {
		rows, err := qx.Query(ctx, q.SQL, q.Args...)
		if err != nil {
			return err
		}
		row, err = pgx.CollectOneRow(rows, pgx.RowToMap)
		return err
	})
	if err != nil {
		// Real error (including an RLS-denied write) logged server-side only
		// in the delivery record; the HTTP caller gets a fixed generic
		// message (AGENTS.md §4: never leak err.Error() into a 500 response).
		h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", rawPayload, eventTypeValue, eventID, err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	row = sanitizeRow(row)
	targetRowID, _ := row["id"].(string)
	h.logDeliveryWithTarget(ctx, wh.ID, http.StatusOK, "inserted", rawPayload, eventTypeValue, eventID, targetRowID, "")
	writeJSON(w, http.StatusOK, map[string]any{"status": "inserted", "id": targetRowID})
}

// parseWebhookPayload normalizes the incoming request into a
// JSON-like map, uniformly regardless of the webhook's configured HTTP
// method (design.md Assumptions: query params for GET, JSON body
// otherwise). An empty/absent body or query string is a valid empty
// payload, not an error (spec Edge Cases).
func parseWebhookPayload(r *http.Request) (map[string]any, error) {
	if r.Method == http.MethodGet {
		payload := make(map[string]any, len(r.URL.Query()))
		for key, values := range r.URL.Query() {
			if len(values) > 0 {
				payload[key] = values[0]
			}
		}
		return payload, nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return map[string]any{}, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// lockEventID takes a Postgres session-level advisory lock keyed by
// (webhookID, eventID), blocking until any other concurrent delivery for
// the same pair has released it. Returns a func that unlocks and releases
// the held connection back to the pool — call it via defer. The lock is
// visible across every connection/replica talking to the same database,
// not just within this process.
func (h *WebhookHandler) lockEventID(ctx context.Context, webhookID, eventID string) (func(), error) {
	h.dedupLockOnce.Do(func() {
		h.dedupLockPool, h.dedupLockErr = h.pool.NewLockPool(ctx, dedupLockPoolMaxConns)
	})
	if h.dedupLockErr != nil {
		return nil, fmt.Errorf("webhook: dedup lock pool: %w", h.dedupLockErr)
	}

	conn, err := h.dedupLockPool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("webhook: acquire connection for dedup lock: %w", err)
	}
	// lock_timeout bounds how long this connection waits on
	// pg_advisory_lock below — without it, a stuck/misbehaving holder
	// (or a bug) blocks this connection, and every other request racing
	// for the same key, forever instead of failing loudly.
	if _, err := conn.Exec(ctx, `SET lock_timeout = '5s'`); err != nil {
		conn.Release()
		return nil, fmt.Errorf("webhook: set lock_timeout: %w", err)
	}
	key := webhookID + ":" + eventID
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1, 0))`, key); err != nil {
		conn.Release()
		return nil, fmt.Errorf("webhook: acquire dedup lock: %w", err)
	}
	return func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, key)
		conn.Release()
	}, nil
}

// logDelivery records one call to a webhook. Logging failures are
// swallowed (best-effort, matches the existing purge/audit pattern in this
// codebase) — a delivery-log write failure must never itself change the
// HTTP response already decided by the caller.
func (h *WebhookHandler) logDelivery(ctx context.Context, webhookID string, httpStatus int, outcome string, rawPayload []byte, eventTypeValue, eventID, errorDetail string) {
	h.logDeliveryWithTarget(ctx, webhookID, httpStatus, outcome, rawPayload, eventTypeValue, eventID, "", errorDetail)
}

// logDeliveryWithTarget is logDelivery's superset, additionally recording
// which row an insert/update/delete affected (target_row_id) — used by the
// active-mode write paths (T7/T8); the simpler branches (capture, invalid
// token, malformed, unmapped, duplicate) never have a target row and go
// through logDelivery instead.
func (h *WebhookHandler) logDeliveryWithTarget(ctx context.Context, webhookID string, httpStatus int, outcome string, rawPayload []byte, eventTypeValue, eventID, targetRowID, errorDetail string) {
	_ = dashboard.InsertDelivery(ctx, h.pool, dashboard.DeliveryEntry{
		WebhookID:      webhookID,
		HTTPStatus:     httpStatus,
		Outcome:        outcome,
		EventTypeValue: eventTypeValue,
		EventID:        eventID,
		RawPayload:     rawPayload,
		TargetRowID:    targetRowID,
		ErrorDetail:    errorDetail,
	})
}
