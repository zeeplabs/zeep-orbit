package dashboard

// builder_usage.go — usage and cost read API (ai-builder-costs C08, ABC-11 to
// ABC-14, ABC-22, ABC-23).
//
//	GET /api/builder/sessions/{id}/usage   SessionUsage   owner
//	GET /api/builder/usage                 UsageReport    own rows; superadmin: everyone's
//	GET /api/builder/usage.csv             UsageCSV       same rule
//
// Every number comes from zeep_system.ai_usage_ledger. Costs are the ones
// stored on the rows when the calls were recorded: nothing here multiplies
// tokens by a price, so a report always equals the sum of its receipts. A NULL
// cost means "no price set" and is never turned into zero: a total carries the
// sum of the priced calls plus the number of calls that have no price.
//
// Ledger rows hold identifiers and counts, no prompt or reply text, and the
// CSV has only those columns. Per-user figures are for superadmins; everyone
// else reads their own rows.

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

const (
	// builderUsageMaxRangeDays caps the date range of a report or an export.
	builderUsageMaxRangeDays = 366
	// builderUsageDefaultRangeDays is the range used when from/to are absent.
	builderUsageDefaultRangeDays = 30
	// builderUsageMaxRows caps the rows of a grouped report.
	builderUsageMaxRows = 1000
	// builderUsageMaxCSVRows caps one export. A larger range is refused, not
	// cut, so an export is never silently incomplete.
	builderUsageMaxCSVRows = 100_000

	builderUsageDateLayout = "2006-01-02"
)

// BuilderUsageTotals is the usage of a set of ledger rows. CostMicros is the
// sum of the calls that have a price, null when none has; UnpricedCalls says
// how many calls are missing from that sum.
type BuilderUsageTotals struct {
	Calls                int64  `json:"calls"`
	InputTokens          int64  `json:"input_tokens"`
	CachedInputTokens    int64  `json:"cached_input_tokens"`
	CacheWriteTokens     int64  `json:"cache_write_tokens"`
	OutputTokens         int64  `json:"output_tokens"`
	TotalTokens          int64  `json:"total_tokens"`
	CostMicros           *int64 `json:"cost_micros"`
	UnpricedCalls        int64  `json:"unpriced_calls"`
	UnreportedCalls      int64  `json:"unreported_calls"`
	CompactionCalls      int64  `json:"compaction_calls"`
	CompactionTokens     int64  `json:"compaction_tokens"`
	CompactionCostMicros *int64 `json:"compaction_cost_micros"`
}

// builderUsageAggCols aggregates ledger rows aliased as l, in the order of
// (*BuilderUsageTotals).dest.
const builderUsageAggCols = `count(*),
	COALESCE(SUM(l.input_tokens::bigint), 0)::bigint,
	COALESCE(SUM(l.cached_input_tokens::bigint), 0)::bigint,
	COALESCE(SUM(l.cache_write_tokens::bigint), 0)::bigint,
	COALESCE(SUM(l.output_tokens::bigint), 0)::bigint,
	SUM(l.cost_micros)::bigint,
	count(*) FILTER (WHERE l.cost_micros IS NULL),
	count(*) FILTER (WHERE NOT l.usage_reported),
	count(*) FILTER (WHERE l.purpose = 'compaction'),
	COALESCE(SUM(l.input_tokens::bigint + l.cached_input_tokens::bigint
	             + l.cache_write_tokens::bigint + l.output_tokens::bigint)
	         FILTER (WHERE l.purpose = 'compaction'), 0)::bigint,
	(SUM(l.cost_micros) FILTER (WHERE l.purpose = 'compaction'))::bigint`

func (t *BuilderUsageTotals) dest() []any {
	return []any{
		&t.Calls, &t.InputTokens, &t.CachedInputTokens, &t.CacheWriteTokens, &t.OutputTokens,
		&t.CostMicros, &t.UnpricedCalls, &t.UnreportedCalls,
		&t.CompactionCalls, &t.CompactionTokens, &t.CompactionCostMicros,
	}
}

func (t *BuilderUsageTotals) finish() {
	t.TotalTokens = t.InputTokens + t.CachedInputTokens + t.CacheWriteTokens + t.OutputTokens
}

// BuilderCapUsage is how much of one cap is used. Used and Limit are tokens
// for the token caps and micros for the cost caps.
type BuilderCapUsage struct {
	Cap   string `json:"cap"`
	Used  int64  `json:"used"`
	Limit int64  `json:"limit"`
}

// BuilderUsageModel is a provider kind and model used by a turn.
type BuilderUsageModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// BuilderTurnUsage is the receipt of one turn.
type BuilderTurnUsage struct {
	TurnID string `json:"turn_id"`
	Seq    int    `json:"seq"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	BuilderUsageTotals
	Models       []BuilderUsageModel `json:"models"`
	FilesChanged int64               `json:"files_changed"`
	BuildsRun    int64               `json:"builds_run"`
	BuildsFailed int64               `json:"builds_failed"`
	// DurationMS is the time the agent worked on the turn, without the time
	// spent waiting for the user to answer a proposal. Null while it runs.
	DurationMS *int64     `json:"duration_ms"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// BuilderSessionUsageDetail is the body of GET /api/builder/sessions/{id}/usage.
type BuilderSessionUsageDetail struct {
	Currency string             `json:"currency"`
	Totals   BuilderUsageTotals `json:"totals"`
	Caps     []BuilderCapUsage  `json:"caps"`
	Turns    []BuilderTurnUsage `json:"turns"`
	// Unattributed is the usage of calls that belong to no turn of the
	// session. Totals = sum of Turns + Unattributed.
	Unattributed *BuilderUsageTotals `json:"unattributed,omitempty"`
}

// BuilderSessionUsage is the usage summary carried by the session views.
type BuilderSessionUsage struct {
	Currency      string            `json:"currency"`
	Calls         int64             `json:"calls"`
	CostMicros    *int64            `json:"cost_micros"`
	UnpricedCalls int64             `json:"unpriced_calls"`
	Caps          []BuilderCapUsage `json:"caps,omitempty"`
}

// builderUsageSeesAll reports whether user may read other users' usage.
func builderUsageSeesAll(user *DashboardUser) bool {
	return HasPlatformPermission(user.Role, ActionManageIntegrations)
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

func builderSessionUsageTotals(ctx context.Context, pool *db.Pool, sessionID string) (*BuilderUsageTotals, error) {
	var t BuilderUsageTotals
	if err := pool.QueryRow(ctx,
		`SELECT `+builderUsageAggCols+` FROM zeep_system.ai_usage_ledger l WHERE l.session_id = $1`, sessionID,
	).Scan(t.dest()...); err != nil {
		return nil, fmt.Errorf("dashboard: session usage totals: %w", err)
	}
	t.finish()
	return &t, nil
}

// builderSessionUsageByTurn returns the usage of a session's calls keyed by
// turn id; calls with no turn are under "".
func builderSessionUsageByTurn(ctx context.Context, pool *db.Pool, sessionID string) (map[string]*BuilderUsageTotals, error) {
	rows, err := pool.Query(ctx,
		`SELECT COALESCE(l.turn_id::text, ''), `+builderUsageAggCols+`
		   FROM zeep_system.ai_usage_ledger l
		  WHERE l.session_id = $1
		  GROUP BY l.turn_id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("dashboard: session usage by turn: %w", err)
	}
	defer rows.Close()
	out := map[string]*BuilderUsageTotals{}
	for rows.Next() {
		var turnID string
		var t BuilderUsageTotals
		if err := rows.Scan(append([]any{&turnID}, t.dest()...)...); err != nil {
			return nil, fmt.Errorf("dashboard: session usage by turn: %w", err)
		}
		t.finish()
		out[turnID] = &t
	}
	return out, rows.Err()
}

// builderSessionModelsByTurn returns the models each turn called, in the
// order they were first used.
func builderSessionModelsByTurn(ctx context.Context, pool *db.Pool, sessionID string) (map[string][]BuilderUsageModel, error) {
	rows, err := pool.Query(ctx,
		`SELECT l.turn_id::text, l.provider, l.model
		   FROM zeep_system.ai_usage_ledger l
		  WHERE l.session_id = $1 AND l.turn_id IS NOT NULL
		  GROUP BY l.turn_id, l.provider, l.model
		  ORDER BY min(l.id)`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("dashboard: session usage models: %w", err)
	}
	defer rows.Close()
	out := map[string][]BuilderUsageModel{}
	for rows.Next() {
		var turnID string
		var m BuilderUsageModel
		if err := rows.Scan(&turnID, &m.Provider, &m.Model); err != nil {
			return nil, fmt.Errorf("dashboard: session usage models: %w", err)
		}
		out[turnID] = append(out[turnID], m)
	}
	return out, rows.Err()
}

// builderTurnActivity is what a turn's events say about its work.
type builderTurnActivity struct {
	filesChanged, buildsRun, buildsFailed int64
}

func builderSessionActivityByTurn(ctx context.Context, pool *db.Pool, sessionID string) (map[string]builderTurnActivity, error) {
	rows, err := pool.Query(ctx,
		`SELECT e.turn_id::text,
		        count(DISTINCT e.payload->>'path') FILTER (WHERE e.type = 'file_changed'),
		        count(*) FILTER (WHERE e.type = 'build_started'),
		        count(*) FILTER (WHERE e.type = 'build_failed')
		   FROM zeep_system.builder_events e
		  WHERE e.session_id = $1 AND e.turn_id IS NOT NULL
		    AND e.type IN ('file_changed', 'build_started', 'build_failed')
		  GROUP BY e.turn_id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("dashboard: session usage activity: %w", err)
	}
	defer rows.Close()
	out := map[string]builderTurnActivity{}
	for rows.Next() {
		var turnID string
		var a builderTurnActivity
		if err := rows.Scan(&turnID, &a.filesChanged, &a.buildsRun, &a.buildsFailed); err != nil {
			return nil, fmt.Errorf("dashboard: session usage activity: %w", err)
		}
		out[turnID] = a
	}
	return out, rows.Err()
}

// builderSessionWorkedMS returns, per turn, the time between each
// turn_started event and the event that ended that run (a proposal, which
// parks the turn until the user answers, or turn_finished). A turn whose last
// run has not ended is absent.
func builderSessionWorkedMS(ctx context.Context, pool *db.Pool, sessionID string) (map[string]int64, error) {
	rows, err := pool.Query(ctx,
		`SELECT e.turn_id::text, e.type, e.created_at
		   FROM zeep_system.builder_events e
		  WHERE e.session_id = $1 AND e.turn_id IS NOT NULL
		    AND e.type IN ('turn_started', 'proposal', 'turn_finished')
		  ORDER BY e.id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("dashboard: session usage duration: %w", err)
	}
	defer rows.Close()
	worked := map[string]int64{}
	open := map[string]time.Time{}
	for rows.Next() {
		var turnID, typ string
		var at time.Time
		if err := rows.Scan(&turnID, &typ, &at); err != nil {
			return nil, fmt.Errorf("dashboard: session usage duration: %w", err)
		}
		start, running := open[turnID]
		switch {
		case typ == "turn_started":
			if !running {
				open[turnID] = at
			}
		case running:
			worked[turnID] += at.Sub(start).Milliseconds()
			delete(open, turnID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for turnID := range open {
		delete(worked, turnID)
	}
	return worked, nil
}

// builderCapUsage returns the caps that apply to a session and how much of
// each is used, in the order of the budget checker. Month figures are the
// owner's builder usage in the current UTC calendar month.
func builderCapUsage(ctx context.Context, pool *db.Pool, s *BuilderSettings, ownerID string, sessionTokens int64, sessionCost *int64, now time.Time) ([]BuilderCapUsage, error) {
	out := []BuilderCapUsage{}
	var monthTokens, monthCost int64
	if s.UserMonthlyTokenCap > 0 || s.UserMonthlyCostCapMicros > 0 {
		now = now.UTC()
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		if err := pool.QueryRow(ctx,
			`SELECT COALESCE(SUM(input_tokens::bigint + cached_input_tokens::bigint
			                     + cache_write_tokens::bigint + output_tokens::bigint), 0)::bigint,
			        COALESCE(SUM(cost_micros), 0)::bigint
			   FROM zeep_system.ai_usage_ledger
			  WHERE user_id = $1 AND feature = $2 AND created_at >= $3 AND created_at < $4`,
			ownerID, store.FeatureBuilder, monthStart, monthStart.AddDate(0, 1, 0),
		).Scan(&monthTokens, &monthCost); err != nil {
			return nil, fmt.Errorf("dashboard: cap usage: %w", err)
		}
	}
	var cost int64
	if sessionCost != nil {
		cost = *sessionCost
	}
	for _, c := range []BuilderCapUsage{
		{store.BudgetCapSessionTokens, sessionTokens, s.SessionTokenCap},
		{store.BudgetCapMonthTokens, monthTokens, s.UserMonthlyTokenCap},
		{store.BudgetCapSessionCost, cost, s.SessionCostCapMicros},
		{store.BudgetCapMonthCost, monthCost, s.UserMonthlyCostCapMicros},
	} {
		if c.Limit > 0 {
			out = append(out, c)
		}
	}
	return out, nil
}

// builderSessionsUsage returns the usage summary of each session id that has
// calls recorded. Sessions with no call are absent.
func builderSessionsUsage(ctx context.Context, pool *db.Pool, sessionIDs []string) (map[string]BuilderSessionUsage, error) {
	out := map[string]BuilderSessionUsage{}
	if len(sessionIDs) == 0 {
		return out, nil
	}
	rows, err := pool.Query(ctx,
		`SELECT l.session_id::text, count(*), SUM(l.cost_micros)::bigint,
		        count(*) FILTER (WHERE l.cost_micros IS NULL)
		   FROM zeep_system.ai_usage_ledger l
		  WHERE l.session_id = ANY($1::text[]::uuid[])
		  GROUP BY l.session_id`, sessionIDs)
	if err != nil {
		return nil, fmt.Errorf("dashboard: sessions usage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var u BuilderSessionUsage
		if err := rows.Scan(&id, &u.Calls, &u.CostMicros, &u.UnpricedCalls); err != nil {
			return nil, fmt.Errorf("dashboard: sessions usage: %w", err)
		}
		out[id] = u
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Session usage
// ---------------------------------------------------------------------------

// SessionUsage handles GET /dashboard/api/builder/sessions/{id}/usage: the
// session's totals, the caps that apply and one receipt per turn. Owner only,
// like the session itself.
func (h *BuilderHandler) SessionUsage(w http.ResponseWriter, r *http.Request) {
	user, sess, ok := h.ownedSession(w, r, false)
	if !ok {
		return
	}
	ctx := r.Context()
	settings, err := GetBuilderSettings(ctx, h.pool)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	turns, err := h.store.ListTurns(ctx, user.ID, sess.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	totals, err := builderSessionUsageTotals(ctx, h.pool, sess.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	byTurn, err := builderSessionUsageByTurn(ctx, h.pool, sess.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	models, err := builderSessionModelsByTurn(ctx, h.pool, sess.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	activity, err := builderSessionActivityByTurn(ctx, h.pool, sess.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	worked, err := builderSessionWorkedMS(ctx, h.pool, sess.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	caps, err := builderCapUsage(ctx, h.pool, settings, sess.OwnerUserID, sess.TokensIn+sess.TokensOut, totals.CostMicros, h.now())
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	out := BuilderSessionUsageDetail{
		Currency: settings.CostCurrency, Totals: *totals, Caps: caps,
		Turns: make([]BuilderTurnUsage, 0, len(turns)),
	}
	for i := range turns {
		t := &turns[i]
		row := BuilderTurnUsage{
			TurnID: t.ID, Seq: t.Seq, Kind: t.Kind, Status: t.Status,
			Models: models[t.ID], CreatedAt: t.CreatedAt, FinishedAt: t.FinishedAt,
		}
		if row.Models == nil {
			row.Models = []BuilderUsageModel{}
		}
		if u := byTurn[t.ID]; u != nil {
			row.BuilderUsageTotals = *u
			delete(byTurn, t.ID)
		}
		a := activity[t.ID]
		row.FilesChanged, row.BuildsRun, row.BuildsFailed = a.filesChanged, a.buildsRun, a.buildsFailed
		if ms, ok := worked[t.ID]; ok {
			row.DurationMS = &ms
		}
		out.Turns = append(out.Turns, row)
	}
	// What is left belongs to no turn of the session.
	if len(byTurn) > 0 {
		rest := BuilderUsageTotals{}
		for _, u := range byTurn {
			addBuilderUsage(&rest, u)
		}
		out.Unattributed = &rest
	}
	writeJSON(w, http.StatusOK, out)
}

// addBuilderUsage adds src to dst. A null cost stays null until a priced call
// is added.
func addBuilderUsage(dst, src *BuilderUsageTotals) {
	dst.Calls += src.Calls
	dst.InputTokens += src.InputTokens
	dst.CachedInputTokens += src.CachedInputTokens
	dst.CacheWriteTokens += src.CacheWriteTokens
	dst.OutputTokens += src.OutputTokens
	dst.UnpricedCalls += src.UnpricedCalls
	dst.UnreportedCalls += src.UnreportedCalls
	dst.CompactionCalls += src.CompactionCalls
	dst.CompactionTokens += src.CompactionTokens
	dst.CostMicros = addMicros(dst.CostMicros, src.CostMicros)
	dst.CompactionCostMicros = addMicros(dst.CompactionCostMicros, src.CompactionCostMicros)
	dst.finish()
}

func addMicros(a, b *int64) *int64 {
	if b == nil {
		return a
	}
	sum := *b
	if a != nil {
		sum += *a
	}
	return &sum
}

// sessionUsageSummary builds the usage block of one session view, with the
// caps that apply to it.
func (h *BuilderHandler) sessionUsageSummary(ctx context.Context, sess *store.Session) (*BuilderSessionUsage, error) {
	settings, err := GetBuilderSettings(ctx, h.pool)
	if err != nil {
		return nil, err
	}
	byID, err := builderSessionsUsage(ctx, h.pool, []string{sess.ID})
	if err != nil {
		return nil, err
	}
	u := byID[sess.ID]
	u.Currency = settings.CostCurrency
	if u.Caps, err = builderCapUsage(ctx, h.pool, settings, sess.OwnerUserID, sess.TokensIn+sess.TokensOut, u.CostMicros, h.now()); err != nil {
		return nil, err
	}
	return &u, nil
}

// ---------------------------------------------------------------------------
// Report and CSV
// ---------------------------------------------------------------------------

// builderUsageFilter is the validated scope of a report or an export: ledger
// rows created in [from, toExclusive), of one user or of everyone.
type builderUsageFilter struct {
	from, to    time.Time // UTC days, both inclusive
	toExclusive time.Time
	userID      *string // nil = every user (superadmin only)
}

const builderUsageWhere = `l.created_at >= $1 AND l.created_at < $2 AND ($3::uuid IS NULL OR l.user_id = $3::uuid)`

func (f *builderUsageFilter) args() []any { return []any{f.from, f.toExclusive, f.userID} }

// usageFilter reads from, to and user_id. It answers the request itself and
// returns ok=false when they are invalid or the caller may not read them.
func (h *BuilderHandler) usageFilter(w http.ResponseWriter, r *http.Request, user *DashboardUser) (*builderUsageFilter, bool) {
	q := r.URL.Query()
	today := h.now().UTC().Truncate(24 * time.Hour)
	f := &builderUsageFilter{to: today}
	var err error
	if v := q.Get("to"); v != "" {
		if f.to, err = time.Parse(builderUsageDateLayout, v); err != nil {
			h.writeError(w, http.StatusBadRequest, "from and to must be dates in YYYY-MM-DD format")
			return nil, false
		}
	}
	f.from = f.to.AddDate(0, 0, -(builderUsageDefaultRangeDays - 1))
	if v := q.Get("from"); v != "" {
		if f.from, err = time.Parse(builderUsageDateLayout, v); err != nil {
			h.writeError(w, http.StatusBadRequest, "from and to must be dates in YYYY-MM-DD format")
			return nil, false
		}
	}
	if f.from.After(f.to) {
		h.writeError(w, http.StatusBadRequest, "from must not be after to")
		return nil, false
	}
	f.toExclusive = f.to.AddDate(0, 0, 1)
	if f.toExclusive.Sub(f.from) > builderUsageMaxRangeDays*24*time.Hour {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("date range must be at most %d days", builderUsageMaxRangeDays))
		return nil, false
	}

	requested := q.Get("user_id")
	if !builderUsageSeesAll(user) {
		// Someone else's usage is the same 404 as someone else's session.
		if requested != "" && requested != user.ID {
			h.writeError(w, http.StatusNotFound, "not found")
			return nil, false
		}
		f.userID = &user.ID
		return f, true
	}
	if requested != "" {
		if !looksLikeUUID(requested) {
			h.writeError(w, http.StatusBadRequest, "user_id must be a UUID")
			return nil, false
		}
		f.userID = &requested
	}
	return f, true
}

// BuilderUsageReportRow is one group of a usage report. Key identifies the
// group: a UTC day, a user id, "provider/model" or a session id ("" for calls
// outside a session).
type BuilderUsageReportRow struct {
	Key      string  `json:"key"`
	Label    *string `json:"label"`
	Provider *string `json:"provider,omitempty"`
	Model    *string `json:"model,omitempty"`
	// Deleted marks a user or a session that no longer exists (or a session
	// that was archived). Its usage still counts.
	Deleted bool `json:"deleted,omitempty"`
	BuilderUsageTotals
}

// BuilderUsageReport is the body of GET /api/builder/usage.
type BuilderUsageReport struct {
	From              string                  `json:"from"`
	To                string                  `json:"to"`
	Timezone          string                  `json:"timezone"`
	GroupBy           string                  `json:"group_by"`
	Currency          string                  `json:"currency"`
	CurrencyChangedAt *time.Time              `json:"currency_changed_at"`
	Totals            BuilderUsageTotals      `json:"totals"`
	Rows              []BuilderUsageReportRow `json:"rows"`
	// Truncated is true when there were more groups than the row limit;
	// Totals always covers the whole range.
	Truncated bool `json:"truncated"`
}

// builderUsageGroups maps group_by to the leading columns (key, label,
// deleted, provider, model), the joins, the GROUP BY and the ORDER BY of the
// report query. $4 is the caller's user id.
var builderUsageGroups = map[string]struct{ cols, join, group, order string }{
	"day": {
		cols:  `to_char(l.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD'), NULL::text, false, NULL::text, NULL::text`,
		group: `1`,
		order: `1`,
	},
	"user": {
		cols:  `l.user_id::text, u.email, (u.id IS NULL), NULL::text, NULL::text`,
		join:  `LEFT JOIN zeep_system.dashboard_users u ON u.id = l.user_id`,
		group: `l.user_id, u.id, u.email`,
		order: `SUM(l.cost_micros) DESC NULLS LAST, count(*) DESC, l.user_id`,
	},
	"model": {
		cols:  `l.provider || '/' || l.model, NULL::text, false, l.provider, l.model`,
		group: `l.provider, l.model`,
		order: `SUM(l.cost_micros) DESC NULLS LAST, count(*) DESC, l.provider, l.model`,
	},
	// A session's title can come from what its owner typed, so it is only
	// returned to that owner.
	"session": {
		cols: `COALESCE(l.session_id::text, ''),
		       CASE WHEN s.owner_user_id = $4::uuid THEN s.title END,
		       (l.session_id IS NOT NULL AND (s.id IS NULL OR s.status = 'archived')), NULL::text, NULL::text`,
		join:  `LEFT JOIN zeep_system.builder_sessions s ON s.id = l.session_id`,
		group: `l.session_id, s.id, s.owner_user_id, s.title, s.status`,
		order: `SUM(l.cost_micros) DESC NULLS LAST, count(*) DESC, l.session_id`,
	},
}

// UsageReport handles GET /dashboard/api/builder/usage?from&to&group_by&user_id:
// usage of a date range (UTC days, both inclusive) grouped by day, user, model
// or session. A superadmin reads everyone's usage; anyone else reads their own
// and may not group by user.
func (h *BuilderHandler) UsageReport(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireUsageReader(w, r)
	if !ok {
		return
	}
	groupBy := r.URL.Query().Get("group_by")
	if groupBy == "" {
		groupBy = "day"
	}
	g, known := builderUsageGroups[groupBy]
	if !known {
		h.writeError(w, http.StatusBadRequest, "group_by must be 'day', 'user', 'model' or 'session'")
		return
	}
	if groupBy == "user" && !builderUsageSeesAll(user) {
		h.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	f, ok := h.usageFilter(w, r, user)
	if !ok {
		return
	}
	ctx := r.Context()
	settings, err := GetBuilderSettings(ctx, h.pool)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	out := BuilderUsageReport{
		From: f.from.Format(builderUsageDateLayout), To: f.to.Format(builderUsageDateLayout), Timezone: "UTC",
		GroupBy: groupBy, Currency: settings.CostCurrency, CurrencyChangedAt: settings.CostCurrencyChangedAt,
		Rows: []BuilderUsageReportRow{},
	}
	if err := h.pool.QueryRow(ctx,
		`SELECT `+builderUsageAggCols+` FROM zeep_system.ai_usage_ledger l WHERE `+builderUsageWhere, f.args()...,
	).Scan(out.Totals.dest()...); err != nil {
		h.serverError(w, r, fmt.Errorf("dashboard: usage report totals: %w", err))
		return
	}
	out.Totals.finish()

	// Only the session grouping reads $4 (the caller); the extra predicate
	// gives the parameter a type in the other groupings.
	rows, err := h.pool.Query(ctx,
		`SELECT `+g.cols+`, `+builderUsageAggCols+`
		   FROM zeep_system.ai_usage_ledger l `+g.join+`
		  WHERE `+builderUsageWhere+` AND $4::uuid IS NOT NULL
		  GROUP BY `+g.group+`
		  ORDER BY `+g.order+`
		  LIMIT `+strconv.Itoa(builderUsageMaxRows+1),
		append(f.args(), user.ID)...)
	if err != nil {
		h.serverError(w, r, fmt.Errorf("dashboard: usage report: %w", err))
		return
	}
	defer rows.Close()
	for rows.Next() {
		var row BuilderUsageReportRow
		if err := rows.Scan(append([]any{&row.Key, &row.Label, &row.Deleted, &row.Provider, &row.Model}, row.dest()...)...); err != nil {
			h.serverError(w, r, fmt.Errorf("dashboard: usage report: %w", err))
			return
		}
		row.finish()
		if len(out.Rows) == builderUsageMaxRows {
			out.Truncated = true
			break
		}
		out.Rows = append(out.Rows, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		h.serverError(w, r, fmt.Errorf("dashboard: usage report: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// requireUsageReader is requireUser plus a database. The report reads the
// ledger only, so it answers on a deployment whose builder is not wired.
func (h *BuilderHandler) requireUsageReader(w http.ResponseWriter, r *http.Request) (*DashboardUser, bool) {
	user, ok := h.requireUser(w, r)
	if !ok {
		return nil, false
	}
	if h.pool == nil {
		h.writeError(w, http.StatusServiceUnavailable, builderNotConfiguredMsg)
		return nil, false
	}
	return user, true
}

// builderUsageCSVHeader is the fixed column list of the export: identifiers,
// counts and the stored cost. cost is cost_micros as a decimal in the
// installation currency; both are empty for a call with no price.
var builderUsageCSVHeader = []string{
	"created_at", "user_id", "session_id", "turn_id", "feature", "provider", "model", "purpose",
	"input_tokens", "cached_input_tokens", "cache_write_tokens", "output_tokens",
	"cost_micros", "cost", "usage_reported",
}

// UsageCSV handles GET /dashboard/api/builder/usage.csv?from&to&user_id: one
// line per recorded call of the range, oldest first, under the same access
// rule as the report.
func (h *BuilderHandler) UsageCSV(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireUsageReader(w, r)
	if !ok {
		return
	}
	f, ok := h.usageFilter(w, r, user)
	if !ok {
		return
	}
	ctx := r.Context()
	var n int64
	if err := h.pool.QueryRow(ctx,
		`SELECT count(*) FROM zeep_system.ai_usage_ledger l WHERE `+builderUsageWhere, f.args()...,
	).Scan(&n); err != nil {
		h.serverError(w, r, fmt.Errorf("dashboard: usage csv count: %w", err))
		return
	}
	if n > builderUsageMaxCSVRows {
		h.writeError(w, http.StatusBadRequest,
			fmt.Sprintf("too many rows to export (limit %d); narrow the date range", builderUsageMaxCSVRows))
		return
	}
	rows, err := h.pool.Query(ctx,
		`SELECT l.created_at, l.user_id::text, COALESCE(l.session_id::text, ''), COALESCE(l.turn_id::text, ''),
		        l.feature, l.provider, l.model, l.purpose,
		        l.input_tokens, l.cached_input_tokens, l.cache_write_tokens, l.output_tokens,
		        l.cost_micros, l.usage_reported
		   FROM zeep_system.ai_usage_ledger l
		  WHERE `+builderUsageWhere+`
		  ORDER BY l.created_at, l.id
		  LIMIT `+strconv.Itoa(builderUsageMaxCSVRows), f.args()...)
	if err != nil {
		h.serverError(w, r, fmt.Errorf("dashboard: usage csv: %w", err))
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ai-usage-%s_%s.csv"`,
		f.from.Format(builderUsageDateLayout), f.to.Format(builderUsageDateLayout)))
	w.Header().Set("Cache-Control", "no-store")
	cw := csv.NewWriter(w)
	_ = cw.Write(builderUsageCSVHeader)
	for rows.Next() {
		var (
			at                                        time.Time
			userID, sessionID, turnID                 string
			feature, provider, model, purpose         string
			input, cached, cacheWrite, output         int64
			cost                                      *int64
			reported                                  bool
			costMicros, costDecimal, reportedAsString = "", "", "false"
		)
		if err := rows.Scan(&at, &userID, &sessionID, &turnID, &feature, &provider, &model, &purpose,
			&input, &cached, &cacheWrite, &output, &cost, &reported); err != nil {
			// The status line is already sent: stop, and leave the log.
			h.logger.Error("builder usage csv: scan failed", zap.Error(err))
			break
		}
		if cost != nil {
			costMicros, costDecimal = strconv.FormatInt(*cost, 10), formatMicrosDecimal(*cost)
		}
		if reported {
			reportedAsString = "true"
		}
		_ = cw.Write([]string{
			at.UTC().Format(time.RFC3339), userID, sessionID, turnID,
			csvSafeCell(feature), csvSafeCell(provider), csvSafeCell(model), csvSafeCell(purpose),
			strconv.FormatInt(input, 10), strconv.FormatInt(cached, 10),
			strconv.FormatInt(cacheWrite, 10), strconv.FormatInt(output, 10),
			costMicros, costDecimal, reportedAsString,
		})
	}
	cw.Flush()
}

// formatMicrosDecimal renders micros as a plain decimal with six places
// ("0.012345"), the form a spreadsheet sums without rounding surprises.
func formatMicrosDecimal(micros int64) string {
	sign := ""
	if micros < 0 {
		sign, micros = "-", -micros
	}
	return fmt.Sprintf("%s%d.%06d", sign, micros/store.MicrosPerUnit, micros%store.MicrosPerUnit)
}
