package dashboard

// builder_usage_test.go — integration tests (real Postgres through
// TEST_DATABASE_URL) for the usage and cost read API (ai-builder-costs C08).

import (
	"context"
	"encoding/csv"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

const buTestModel = "test-model"

// buPrice prices buTestModel: 3 per million input tokens, 0.3 cached, 15 output.
func (e *bhEnv) buPrice(t *testing.T) {
	t.Helper()
	cached := "0.3"
	if _, err := UpsertBuilderPrice(context.Background(), e.pool, e.super.ID, &BuilderModelPrice{
		Provider: aiProviderOpenAICompatible, Model: buTestModel, Input: "3", CachedInput: &cached, Output: "15",
	}); err != nil {
		t.Fatalf("seed price: %v", err)
	}
}

// buTurn inserts a finished turn and returns its id.
func (e *bhEnv) buTurn(t *testing.T, sessionID string, seq int) string {
	t.Helper()
	var id string
	if err := e.pool.QueryRow(context.Background(),
		`INSERT INTO zeep_system.builder_turns (session_id, seq, kind, status, finished_at)
		 VALUES ($1, $2, 'chat', 'completed', now()) RETURNING id`, sessionID, seq).Scan(&id); err != nil {
		t.Fatalf("seed turn: %v", err)
	}
	return id
}

// buCall records one call and returns its ledger id.
func (e *bhEnv) buCall(t *testing.T, r store.UsageRecord) int64 {
	t.Helper()
	if r.Feature == "" {
		r.Feature = store.FeatureBuilder
	}
	if r.Provider == "" {
		r.Provider = aiProviderOpenAICompatible
	}
	if r.Model == "" {
		r.Model = buTestModel
	}
	if err := e.st.RecordUsage(context.Background(), r); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	var id int64
	if err := e.pool.QueryRow(context.Background(), `SELECT max(id) FROM zeep_system.ai_usage_ledger`).Scan(&id); err != nil {
		t.Fatalf("ledger id: %v", err)
	}
	return id
}

// buCallAt is buCall with the row moved to at.
func (e *bhEnv) buCallAt(t *testing.T, r store.UsageRecord, at time.Time) {
	t.Helper()
	id := e.buCall(t, r)
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE zeep_system.ai_usage_ledger SET created_at = $2 WHERE id = $1`, id, at); err != nil {
		t.Fatalf("move ledger row: %v", err)
	}
}

func (e *bhEnv) buEvent(t *testing.T, sessionID, turnID, typ, payload string, at time.Time) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO zeep_system.builder_events (session_id, turn_id, type, payload, created_at)
		 VALUES ($1, $2, $3, $4::jsonb, $5)`, sessionID, turnID, typ, payload, at); err != nil {
		t.Fatalf("seed event: %v", err)
	}
}

func (e *bhEnv) buSessionUsage(t *testing.T, user *DashboardUser, id string) (int, BuilderSessionUsageDetail) {
	t.Helper()
	w := e.do(t, e.h.SessionUsage, user, http.MethodGet, "/x", "", "id", id)
	if w.Code != http.StatusOK {
		return w.Code, BuilderSessionUsageDetail{}
	}
	return w.Code, decodeJSON[BuilderSessionUsageDetail](t, w)
}

func (e *bhEnv) buReport(t *testing.T, user *DashboardUser, query string) (int, BuilderUsageReport) {
	t.Helper()
	w := e.do(t, e.h.UsageReport, user, http.MethodGet, "/x?"+query, "")
	if w.Code != http.StatusOK {
		return w.Code, BuilderUsageReport{}
	}
	return w.Code, decodeJSON[BuilderUsageReport](t, w)
}

func buCost(t *testing.T, v *int64) int64 {
	t.Helper()
	if v == nil {
		t.Fatalf("cost is null, want a number")
	}
	return *v
}

func buReportRow(t *testing.T, rep BuilderUsageReport, key string) BuilderUsageReportRow {
	t.Helper()
	for _, r := range rep.Rows {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("no row with key %q in %+v", key, rep.Rows)
	return BuilderUsageReportRow{}
}

func TestBuilderUsage_SessionTotalsAndReceipts(t *testing.T) {
	e := newBHEnv(t)
	e.buPrice(t)
	costCap := int64(50_000)
	e.setSettings(t, BuilderSettingsInput{SessionCostCapMicros: &costCap})
	sess := e.createOK(t, e.owner, e.appA)
	turnA, turnB := e.buTurn(t, sess.ID, 101), e.buTurn(t, sess.ID, 102)
	base := store.UsageRecord{UserID: e.owner.ID, SessionID: sess.ID, Reported: true}

	a1 := base
	a1.TurnID, a1.InputTokens, a1.CachedInputTokens, a1.OutputTokens = turnA, 1000, 200, 500
	e.buCall(t, a1) // 3000 + 60 + 7500 = 10560 micros
	a2 := base
	a2.TurnID, a2.Purpose, a2.InputTokens, a2.OutputTokens = turnA, store.PurposeCompaction, 400, 100
	e.buCall(t, a2) // 1200 + 1500 = 2700 micros
	b1 := base
	b1.TurnID, b1.Model, b1.InputTokens, b1.OutputTokens = turnB, "no-price", 100, 50
	e.buCall(t, b1) // no price
	b2 := base
	b2.TurnID, b2.Reported = turnB, false
	e.buCall(t, b2) // provider sent no usage: cost 0
	loose := base
	loose.InputTokens, loose.OutputTokens = 10, 5
	e.buCall(t, loose) // 30 + 75 = 105 micros, no turn

	t0 := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	e.buEvent(t, sess.ID, turnA, "turn_started", `{"seq":101}`, t0)
	e.buEvent(t, sess.ID, turnA, "file_changed", `{"path":"src/a.tsx","op":"write"}`, t0)
	e.buEvent(t, sess.ID, turnA, "file_changed", `{"path":"src/a.tsx","op":"write"}`, t0)
	e.buEvent(t, sess.ID, turnA, "file_changed", `{"path":"src/b.tsx","op":"write"}`, t0)
	e.buEvent(t, sess.ID, turnA, "build_started", `{"step":"build"}`, t0)
	e.buEvent(t, sess.ID, turnA, "build_failed", `{"step":"build"}`, t0)
	e.buEvent(t, sess.ID, turnA, "build_started", `{"step":"build"}`, t0)
	e.buEvent(t, sess.ID, turnA, "proposal", `{}`, t0.Add(10*time.Second))
	e.buEvent(t, sess.ID, turnA, "turn_started", `{"seq":101,"resumed":true}`, t0.Add(70*time.Second))
	e.buEvent(t, sess.ID, turnA, "turn_finished", `{"status":"completed"}`, t0.Add(75*time.Second))
	e.buEvent(t, sess.ID, turnB, "turn_started", `{"seq":102}`, t0.Add(80*time.Second))

	code, got := e.buSessionUsage(t, e.owner, sess.ID)
	if code != http.StatusOK {
		t.Fatalf("session usage: want 200, got %d", code)
	}

	// Totals equal the ledger sums.
	var calls, tokens, cost int64
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*), SUM(input_tokens + cached_input_tokens + cache_write_tokens + output_tokens)::bigint,
		        SUM(cost_micros)::bigint
		 FROM zeep_system.ai_usage_ledger WHERE session_id = $1`, sess.ID).Scan(&calls, &tokens, &cost); err != nil {
		t.Fatalf("ledger sums: %v", err)
	}
	if got.Totals.Calls != calls || got.Totals.TotalTokens != tokens || buCost(t, got.Totals.CostMicros) != cost {
		t.Errorf("totals = %+v, ledger says calls=%d tokens=%d cost=%d", got.Totals, calls, tokens, cost)
	}
	if cost != 10560+2700+105 {
		t.Errorf("ledger cost = %d, want %d", cost, 10560+2700+105)
	}
	if got.Totals.UnpricedCalls != 1 || got.Totals.UnreportedCalls != 1 || got.Totals.CompactionCalls != 1 ||
		got.Totals.CompactionTokens != 500 || buCost(t, got.Totals.CompactionCostMicros) != 2700 {
		t.Errorf("totals breakdown = %+v", got.Totals)
	}
	if got.Totals.InputTokens != 1510 || got.Totals.CachedInputTokens != 200 || got.Totals.OutputTokens != 655 {
		t.Errorf("token classes = %+v", got.Totals)
	}
	if got.Currency != "USD" {
		t.Errorf("currency = %q, want USD", got.Currency)
	}

	// Totals = sum of the receipts + the calls outside a turn.
	if len(got.Turns) != 2 || got.Unattributed == nil {
		t.Fatalf("want 2 turns and unattributed usage, got %d turns, unattributed=%v", len(got.Turns), got.Unattributed)
	}
	sum := *got.Unattributed
	for i := range got.Turns {
		addBuilderUsage(&sum, &got.Turns[i].BuilderUsageTotals)
	}
	if sum.Calls != got.Totals.Calls || sum.TotalTokens != got.Totals.TotalTokens || buCost(t, sum.CostMicros) != cost {
		t.Errorf("receipts sum to %+v, totals are %+v", sum, got.Totals)
	}

	ra := got.Turns[0]
	if ra.TurnID != turnA || ra.Seq != 101 || ra.Calls != 2 || buCost(t, ra.CostMicros) != 13260 || ra.UnpricedCalls != 0 {
		t.Errorf("turn A receipt = %+v", ra)
	}
	if ra.FilesChanged != 2 || ra.BuildsRun != 2 || ra.BuildsFailed != 1 {
		t.Errorf("turn A activity: files=%d builds=%d failed=%d, want 2/2/1", ra.FilesChanged, ra.BuildsRun, ra.BuildsFailed)
	}
	if ra.DurationMS == nil || *ra.DurationMS != 15000 {
		t.Errorf("turn A duration = %v, want 15000 (the wait for the proposal is not counted)", ra.DurationMS)
	}
	if len(ra.Models) != 1 || ra.Models[0] != (BuilderUsageModel{aiProviderOpenAICompatible, buTestModel}) {
		t.Errorf("turn A models = %+v", ra.Models)
	}

	// Partly priced turn: the known cost and the number of calls missing from it.
	rb := got.Turns[1]
	if rb.Calls != 2 || rb.UnpricedCalls != 1 || buCost(t, rb.CostMicros) != 0 || rb.UnreportedCalls != 1 {
		t.Errorf("turn B receipt = %+v", rb)
	}
	if rb.DurationMS != nil {
		t.Errorf("turn B is still running, duration = %d", *rb.DurationMS)
	}
	if len(rb.Models) != 2 {
		t.Errorf("turn B models = %+v, want 2", rb.Models)
	}

	caps := map[string]BuilderCapUsage{}
	for _, c := range got.Caps {
		caps[c.Cap] = c
	}
	if c := caps[store.BudgetCapSessionTokens]; c.Used != tokens || c.Limit != 1_000_000 {
		t.Errorf("session token cap = %+v", c)
	}
	if c := caps[store.BudgetCapMonthTokens]; c.Used != tokens || c.Limit != 10_000_000 {
		t.Errorf("month token cap = %+v", c)
	}
	if c := caps[store.BudgetCapSessionCost]; c.Used != cost || c.Limit != costCap {
		t.Errorf("session cost cap = %+v", c)
	}
	if _, ok := caps[store.BudgetCapMonthCost]; ok {
		t.Errorf("month cost cap is not set and must not be listed")
	}
}

func TestBuilderUsage_SessionWithOnlyUnpricedCallsHasNullCost(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.owner, e.appA)
	turn := e.buTurn(t, sess.ID, 101)
	e.buCall(t, store.UsageRecord{UserID: e.owner.ID, SessionID: sess.ID, TurnID: turn, InputTokens: 10, OutputTokens: 5, Reported: true})

	code, got := e.buSessionUsage(t, e.owner, sess.ID)
	if code != http.StatusOK {
		t.Fatalf("want 200, got %d", code)
	}
	if got.Totals.CostMicros != nil || got.Totals.UnpricedCalls != 1 || got.Turns[0].CostMicros != nil {
		t.Errorf("no price set must be a null cost, never zero: %+v", got.Totals)
	}
	if got.Unattributed != nil {
		t.Errorf("unattributed = %+v, want none", got.Unattributed)
	}
}

func TestBuilderUsage_SessionIsOwnerOnly(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.owner, e.appA)
	for name, user := range map[string]*DashboardUser{"other user": e.other, "viewer of the app": e.viewer, "superadmin": e.super} {
		if code, _ := e.buSessionUsage(t, user, sess.ID); code != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d", name, code)
		}
	}
	if code, _ := e.buSessionUsage(t, nil, sess.ID); code != http.StatusUnauthorized {
		t.Errorf("anonymous: want 401, got %d", code)
	}
	if code, _ := e.buSessionUsage(t, e.owner, "not-a-uuid"); code != http.StatusNotFound {
		t.Errorf("malformed id: want 404, got %d", code)
	}
	if w := e.del(t, e.owner, sess.ID); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}
	if code, _ := e.buSessionUsage(t, e.owner, sess.ID); code != http.StatusNotFound {
		t.Errorf("archived session: want 404, got %d", code)
	}
}

// buReportEnv seeds two users over two days. The owner: 2 calls in session s1
// on day 1 (priced) and 1 on day 2 (no price). The other user: 1 priced call
// on day 2 in a session that is then deleted.
func buReportEnv(t *testing.T) (e *bhEnv, s1, s2 string) {
	t.Helper()
	e = newBHEnv(t)
	e.buPrice(t)
	d1 := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 3, 11, 23, 59, 0, 0, time.UTC)
	s1 = e.createOK(t, e.owner, e.appA).ID
	s2 = e.createOK(t, e.other, e.appB).ID
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE zeep_system.builder_sessions SET title = 'My shop' WHERE id = $1`, s1); err != nil {
		t.Fatalf("title: %v", err)
	}
	own := store.UsageRecord{UserID: e.owner.ID, SessionID: s1, InputTokens: 1000, OutputTokens: 100, Reported: true}
	e.buCallAt(t, own, d1) // 3000 + 1500 = 4500
	compaction := own
	compaction.Purpose = store.PurposeCompaction
	e.buCallAt(t, compaction, d1) // 4500
	unpriced := own
	unpriced.Model = "no-price"
	e.buCallAt(t, unpriced, d2)
	e.buCallAt(t, store.UsageRecord{UserID: e.other.ID, SessionID: s2, InputTokens: 2000, OutputTokens: 0, Reported: true}, d2) // 6000
	if _, err := e.pool.Exec(context.Background(), `DELETE FROM zeep_system.builder_sessions WHERE id = $1`, s2); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	return e, s1, s2
}

const buRange = "from=2026-03-01&to=2026-03-31"

func TestBuilderUsage_ReportForARegularUserIsTheirOwn(t *testing.T) {
	e, s1, _ := buReportEnv(t)

	code, rep := e.buReport(t, e.owner, buRange)
	if code != http.StatusOK {
		t.Fatalf("report: want 200, got %d", code)
	}
	if rep.GroupBy != "day" || rep.Currency != "USD" || rep.Timezone != "UTC" || rep.From != "2026-03-01" || rep.To != "2026-03-31" {
		t.Errorf("report header = %+v", rep)
	}
	if rep.Totals.Calls != 3 || buCost(t, rep.Totals.CostMicros) != 9000 || rep.Totals.UnpricedCalls != 1 ||
		rep.Totals.CompactionCalls != 1 || buCost(t, rep.Totals.CompactionCostMicros) != 4500 {
		t.Errorf("own totals = %+v", rep.Totals)
	}
	if len(rep.Rows) != 2 || rep.Rows[0].Key != "2026-03-10" || rep.Rows[1].Key != "2026-03-11" {
		t.Fatalf("day rows = %+v", rep.Rows)
	}
	if rep.Rows[0].Calls != 2 || rep.Rows[1].Calls != 1 || rep.Rows[1].CostMicros != nil {
		t.Errorf("day rows = %+v", rep.Rows)
	}

	if code, _ := e.buReport(t, e.owner, buRange+"&group_by=user"); code != http.StatusForbidden {
		t.Errorf("group by user as a regular user: want 403, got %d", code)
	}
	if code, _ := e.buReport(t, e.owner, buRange+"&user_id="+e.other.ID); code != http.StatusNotFound {
		t.Errorf("another user's usage: want 404, got %d", code)
	}
	if code, rep := e.buReport(t, e.owner, buRange+"&user_id="+e.owner.ID); code != http.StatusOK || rep.Totals.Calls != 3 {
		t.Errorf("own user_id: code=%d calls=%d", code, rep.Totals.Calls)
	}
	if code, _ := e.buReport(t, nil, buRange); code != http.StatusUnauthorized {
		t.Errorf("anonymous: want 401, got %d", code)
	}

	// Own session: the title is shown.
	_, rep = e.buReport(t, e.owner, buRange+"&group_by=session")
	if len(rep.Rows) != 1 {
		t.Fatalf("session rows = %+v", rep.Rows)
	}
	if r := rep.Rows[0]; r.Key != s1 || r.Label == nil || *r.Label != "My shop" || r.Deleted {
		t.Errorf("own session row = %+v", r)
	}
}

func TestBuilderUsage_ReportForASuperadminCoversEveryone(t *testing.T) {
	e, s1, s2 := buReportEnv(t)

	code, rep := e.buReport(t, e.super, buRange+"&group_by=user")
	if code != http.StatusOK {
		t.Fatalf("report: want 200, got %d", code)
	}
	if rep.Totals.Calls != 4 || buCost(t, rep.Totals.CostMicros) != 15000 {
		t.Errorf("totals = %+v", rep.Totals)
	}
	if len(rep.Rows) != 2 {
		t.Fatalf("user rows = %+v", rep.Rows)
	}
	owner := buReportRow(t, rep, e.owner.ID)
	if owner.Label == nil || *owner.Label != e.owner.Email || owner.Calls != 3 || buCost(t, owner.CostMicros) != 9000 {
		t.Errorf("owner row = %+v", owner)
	}
	if other := buReportRow(t, rep, e.other.ID); other.Calls != 1 || buCost(t, other.CostMicros) != 6000 {
		t.Errorf("other row = %+v", other)
	}

	// A deleted session still counts; another user's title is not shown.
	_, rep = e.buReport(t, e.super, buRange+"&group_by=session")
	if gone := buReportRow(t, rep, s2); !gone.Deleted || gone.Label != nil || buCost(t, gone.CostMicros) != 6000 {
		t.Errorf("deleted session row = %+v", gone)
	}
	if live := buReportRow(t, rep, s1); live.Deleted || live.Label != nil || live.Calls != 3 {
		t.Errorf("live session row = %+v", live)
	}

	_, rep = e.buReport(t, e.super, buRange+"&group_by=model")
	priced := buReportRow(t, rep, aiProviderOpenAICompatible+"/"+buTestModel)
	if priced.Calls != 3 || priced.Provider == nil || *priced.Provider != aiProviderOpenAICompatible || priced.Model == nil || *priced.Model != buTestModel {
		t.Errorf("model row = %+v", priced)
	}
	if unpriced := buReportRow(t, rep, aiProviderOpenAICompatible+"/no-price"); unpriced.CostMicros != nil || unpriced.UnpricedCalls != 1 {
		t.Errorf("unpriced model row = %+v", unpriced)
	}

	if _, rep := e.buReport(t, e.super, buRange+"&user_id="+e.other.ID); rep.Totals.Calls != 1 {
		t.Errorf("filter by user: calls = %d, want 1", rep.Totals.Calls)
	}
	// Both ends of the range are whole UTC days.
	if _, rep := e.buReport(t, e.super, "from=2026-03-11&to=2026-03-11"); rep.Totals.Calls != 2 {
		t.Errorf("single day: calls = %d, want 2", rep.Totals.Calls)
	}
	if _, rep := e.buReport(t, e.super, "from=2026-03-12&to=2026-03-20"); rep.Totals.Calls != 0 || rep.Totals.CostMicros != nil || len(rep.Rows) != 0 {
		t.Errorf("empty range = %+v", rep)
	}
}

func TestBuilderUsage_ReportValidation(t *testing.T) {
	e := newBHEnv(t)
	cases := map[string]string{
		"range over the cap": "from=2025-03-01&to=2026-03-02",
		"malformed from":     "from=10/03/2026&to=2026-03-31",
		"malformed to":       "from=2026-03-01&to=yesterday",
		"from after to":      "from=2026-03-31&to=2026-03-01",
		"unknown grouping":   buRange + "&group_by=app",
		"malformed user_id":  buRange + "&user_id=abc",
	}
	for name, q := range cases {
		w := e.do(t, e.h.UsageReport, e.super, http.MethodGet, "/x?"+q, "")
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d: %s", name, w.Code, w.Body.String())
		}
	}
	// 366 days are allowed; the default range needs no parameters.
	if code, _ := e.buReport(t, e.super, "from=2025-03-01&to=2026-03-01"); code != http.StatusOK {
		t.Errorf("366 days: want 200, got %d", code)
	}
	if code, rep := e.buReport(t, e.super, ""); code != http.StatusOK || rep.From == "" || rep.To == "" {
		t.Errorf("default range: code=%d rep=%+v", code, rep)
	}
}

func TestBuilderUsage_ReportWorksWithoutABuilder(t *testing.T) {
	e := newBHEnv(t, bhWithoutBuilder())
	if code, _ := e.buReport(t, e.super, buRange); code != http.StatusOK {
		t.Errorf("report without a builder: want 200, got %d", code)
	}
}

func buCSV(t *testing.T, e *bhEnv, user *DashboardUser, query string) [][]string {
	t.Helper()
	w := e.do(t, e.h.UsageCSV, user, http.MethodGet, "/x?"+query, "")
	if w.Code != http.StatusOK {
		t.Fatalf("csv: want 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("content type = %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".csv") {
		t.Errorf("content disposition = %q", cd)
	}
	recs, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	return recs
}

func TestBuilderUsage_CSV(t *testing.T) {
	e, s1, _ := buReportEnv(t)
	e.buCallAt(t, store.UsageRecord{UserID: e.owner.ID, Model: "=HYPERLINK(1)", InputTokens: 1, Reported: true},
		time.Date(2026, 3, 12, 0, 0, 0, 0, time.UTC))

	recs := buCSV(t, e, e.super, buRange)
	wantHeader := "created_at,user_id,session_id,turn_id,feature,provider,model,purpose," +
		"input_tokens,cached_input_tokens,cache_write_tokens,output_tokens,cost_micros,cost,usage_reported"
	if got := strings.Join(recs[0], ","); got != wantHeader {
		t.Errorf("header = %s\nwant     %s", got, wantHeader)
	}
	if len(recs) != 1+5 {
		t.Fatalf("want 5 data rows, got %d", len(recs)-1)
	}
	first := recs[1]
	if first[0] != "2026-03-10T12:00:00Z" || first[1] != e.owner.ID || first[2] != s1 || first[3] != "" ||
		first[4] != store.FeatureBuilder || first[6] != buTestModel || first[7] != store.PurposeChat ||
		first[8] != "1000" || first[11] != "100" || first[12] != "4500" || first[13] != "0.004500" || first[14] != "true" {
		t.Errorf("first row = %v", first)
	}
	var unpriced, guarded bool
	for _, r := range recs[1:] {
		if r[6] == "no-price" {
			unpriced = r[12] == "" && r[13] == ""
		}
		if r[6] == "'=HYPERLINK(1)" {
			guarded = true
		}
	}
	if !unpriced {
		t.Errorf("a call with no price must have empty cost cells")
	}
	if !guarded {
		t.Errorf("a model name starting with '=' must not reach a spreadsheet as a formula")
	}

	// A regular user exports their own rows only.
	for _, r := range buCSV(t, e, e.owner, buRange)[1:] {
		if r[1] != e.owner.ID {
			t.Errorf("row of another user in the owner's export: %v", r)
		}
	}
	if w := e.do(t, e.h.UsageCSV, e.owner, http.MethodGet, "/x?"+buRange+"&user_id="+e.other.ID, ""); w.Code != http.StatusNotFound {
		t.Errorf("another user's export: want 404, got %d", w.Code)
	}
	if w := e.do(t, e.h.UsageCSV, e.super, http.MethodGet, "/x?from=2025-03-01&to=2026-03-02", ""); w.Code != http.StatusBadRequest {
		t.Errorf("range over the cap: want 400, got %d", w.Code)
	}
	if w := e.do(t, e.h.UsageCSV, nil, http.MethodGet, "/x?"+buRange, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: want 401, got %d", w.Code)
	}
}

func TestBuilderUsage_CSVRefusesAnExportOverTheRowLimit(t *testing.T) {
	e := newBHEnv(t)
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO zeep_system.ai_usage_ledger (user_id, feature, provider, model, input_tokens, output_tokens, created_at)
		 SELECT $1, 'builder', 'openai', 'm', 1, 1, '2026-03-10T00:00:00Z'::timestamptz
		 FROM generate_series(1, $2)`, e.owner.ID, builderUsageMaxCSVRows+1); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
	w := e.do(t, e.h.UsageCSV, e.super, http.MethodGet, "/x?"+buRange, "")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "narrow the date range") {
		t.Errorf("want 400 asking for a narrower range, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBuilderUsage_SessionViewsCarryTheCost(t *testing.T) {
	e := newBHEnv(t)
	e.buPrice(t)
	used := e.createOK(t, e.owner, e.appA)
	fresh := e.createOK(t, e.owner, e.appA)
	e.buCall(t, store.UsageRecord{UserID: e.owner.ID, SessionID: used.ID, InputTokens: 1000, OutputTokens: 100, Reported: true})
	e.buCall(t, store.UsageRecord{UserID: e.owner.ID, SessionID: used.ID, Model: "no-price", InputTokens: 1, Reported: true})

	w := e.get(t, e.owner, used.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("get session: %d", w.Code)
	}
	detail := decodeJSON[BuilderSessionDetail](t, w)
	if u := detail.Usage; u == nil || u.Currency != "USD" || u.Calls != 2 || buCost(t, u.CostMicros) != 4500 || u.UnpricedCalls != 1 {
		t.Fatalf("detail usage = %+v", detail.Usage)
	}
	if len(detail.Usage.Caps) != 2 || detail.Usage.Caps[0] != (BuilderCapUsage{store.BudgetCapSessionTokens, 1101, 1_000_000}) {
		t.Errorf("detail caps = %+v", detail.Usage.Caps)
	}

	w = e.do(t, e.h.ListSessions, e.owner, http.MethodGet, "/x", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list sessions: %d", w.Code)
	}
	byID := map[string]*BuilderSessionUsage{}
	for _, s := range decodeJSON[[]BuilderSessionView](t, w) {
		byID[s.ID] = s.Usage
	}
	if u := byID[used.ID]; u == nil || u.Calls != 2 || buCost(t, u.CostMicros) != 4500 || u.UnpricedCalls != 1 || u.Currency != "USD" {
		t.Errorf("list usage of the used session = %+v", u)
	}
	if u := byID[fresh.ID]; u == nil || u.Calls != 0 || u.CostMicros != nil || u.Currency != "USD" {
		t.Errorf("list usage of the fresh session = %+v", u)
	}
}

func TestBuilderUsage_ReportGroupedByPurpose(t *testing.T) {
	e, s1, _ := buReportEnv(t)
	planning := store.UsageRecord{UserID: e.owner.ID, SessionID: s1, Purpose: store.PurposePlanning, InputTokens: 500, OutputTokens: 100, Reported: true}
	e.buCallAt(t, planning, time.Date(2026, 3, 10, 13, 0, 0, 0, time.UTC)) // 1500 + 1500 = 3000

	_, rep := e.buReport(t, e.super, buRange+"&group_by=purpose")
	if rep.GroupBy != "purpose" || len(rep.Rows) != 3 {
		t.Fatalf("purpose report = %+v", rep)
	}
	chat := buReportRow(t, rep, store.PurposeChat)
	if chat.Calls != 3 || buCost(t, chat.CostMicros) != 10500 || chat.UnpricedCalls != 1 {
		t.Errorf("chat row = %+v", chat)
	}
	if c := buReportRow(t, rep, store.PurposeCompaction); c.Calls != 1 || buCost(t, c.CostMicros) != 4500 {
		t.Errorf("compaction row = %+v", c)
	}
	if p := buReportRow(t, rep, store.PurposePlanning); p.Calls != 1 || buCost(t, p.CostMicros) != 3000 {
		t.Errorf("planning row = %+v", p)
	}
	var calls, cost int64
	for _, r := range rep.Rows {
		calls += r.Calls
		cost += buCost(t, r.CostMicros)
	}
	if calls != rep.Totals.Calls || cost != buCost(t, rep.Totals.CostMicros) {
		t.Errorf("rows add up to %d calls / %d micros, totals = %+v", calls, cost, rep.Totals)
	}

	// A regular user sees only their own rows.
	_, own := e.buReport(t, e.owner, buRange+"&group_by=purpose")
	if chat := buReportRow(t, own, store.PurposeChat); chat.Calls != 2 {
		t.Errorf("owner chat row = %+v", chat)
	}

	// The 400 message names every grouping.
	w := e.do(t, e.h.UsageReport, e.super, http.MethodGet, "/x?"+buRange+"&group_by=nope", "")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "'purpose'") {
		t.Errorf("unknown group_by: code=%d body=%s", w.Code, w.Body.String())
	}
}
