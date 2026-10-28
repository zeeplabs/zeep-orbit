package dashboard

// builder_skills_store.go: the AI Builder's skills (ai-builder-skills S06).
//
// Two sources feed one set. Built-ins are embedded in the binary (immutable by
// construction); the database only keeps an enabled override per built-in name
// (zeep_system.builder_skill_builtin_state, no row = enabled). Custom skills are
// superadmin uploads in zeep_system.builder_skills, versioned append-only in
// builder_skill_versions; the active version is the highest one.
//
// Every write runs in one transaction behind an advisory lock, like
// SaveBuilderPrompt, so the index-size check cannot race with another upload
// or enable.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/agent"
	"github.com/zeeplabs/zeep-orbit/internal/agent/skills"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

var (
	// ErrBuilderSkillBuiltin: a built-in skill cannot be created over,
	// edited or deleted. Only its enabled flag can change.
	ErrBuilderSkillBuiltin = errors.New("built-in skills are read-only")
	// ErrBuilderSkillReserved: custom skills cannot use the built-in prefix.
	ErrBuilderSkillReserved = errors.New("skill name uses the reserved built-in prefix")
	// ErrBuilderSkillNotFound: no live skill with that name.
	ErrBuilderSkillNotFound = errors.New("skill not found")
)

// BuilderSkillsBudgetError is returned when a change would make the skills
// index larger than the agent can carry (agent.MaxSkillsIndexChars).
type BuilderSkillsBudgetError struct {
	Chars int
	Max   int
}

func (e *BuilderSkillsBudgetError) Error() string {
	return fmt.Sprintf("skills index would be %d characters, the limit is %d", e.Chars, e.Max)
}

// builderBuiltinSkills returns the embedded built-ins. It is a variable so
// tests can supply fixtures: the real set only exists once the curated skills
// are authored.
var builderBuiltinSkills = skills.Builtins

func builtinSkillByName(name string) (skills.Skill, bool) {
	for _, s := range builderBuiltinSkills() {
		if s.Name == name {
			return s, true
		}
	}
	return skills.Skill{}, false
}

// BuilderSkillSummary is the API view of one skill in the list.
type BuilderSkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Source is "built-in" or "custom".
	Source  string `json:"source"`
	Enabled bool   `json:"enabled"`
	// Locked is true for built-ins: no edit, no delete, only the toggle.
	Locked      bool   `json:"locked"`
	Version     int    `json:"version"`
	License     string `json:"license"`
	Provenance  string `json:"provenance,omitempty"`
	InAgentView bool   `json:"in_agent_view"`
	// Chars is what the skill adds to the index when enabled.
	Chars          int        `json:"chars"`
	UpdatedAt      *time.Time `json:"updated_at"`
	UpdatedByEmail *string    `json:"updated_by_email"`
}

// BuilderSkillsIndexInfo is the index size against its budget.
type BuilderSkillsIndexInfo struct {
	Chars   int      `json:"chars"`
	Max     int      `json:"max"`
	Dropped []string `json:"dropped"`
}

// BuilderSkillVersionInfo is one stored version of a custom skill.
type BuilderSkillVersionInfo struct {
	Version        int       `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	CreatedByEmail string    `json:"created_by_email"`
	ContentSHA256  string    `json:"content_sha256"`
}

// BuilderSkillDetail is one skill with its body (and history, if custom).
type BuilderSkillDetail struct {
	BuilderSkillSummary
	Body     string                    `json:"body"`
	Versions []BuilderSkillVersionInfo `json:"versions,omitempty"`
}

// BuilderSkillSaveResult is the outcome of SaveBuilderSkill.
type BuilderSkillSaveResult struct {
	Name    string
	Version int
	// Created is false when the content equals the active version (no write).
	Created bool
	// NewLineage is true for the first version of a skill (v1).
	NewLineage bool
	SHA256     string
	// Chars is the body size in characters, for the audit row (never the body).
	Chars int
}

type skillRow struct {
	skills.Entry
	license        string
	provenance     string
	locked         bool
	updatedAt      *time.Time
	updatedByEmail *string
}

// loadSkillRows returns every built-in and every live custom skill, enabled or
// not, with descriptions only (never bodies).
func loadSkillRows(ctx context.Context, q db.Querier) ([]skillRow, error) {
	type state struct {
		enabled bool
		at      time.Time
		email   string
	}
	states := map[string]state{}
	srows, err := q.Query(ctx,
		`SELECT s.name, s.enabled, s.updated_at, COALESCE(u.email, '')
		 FROM zeep_system.builder_skill_builtin_state s
		 LEFT JOIN zeep_system.dashboard_users u ON u.id = s.updated_by`)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list builtin skill state: %w", err)
	}
	for srows.Next() {
		var name string
		var st state
		if err := srows.Scan(&name, &st.enabled, &st.at, &st.email); err != nil {
			srows.Close()
			return nil, fmt.Errorf("dashboard: scan builtin skill state: %w", err)
		}
		states[name] = st
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard: builtin skill state rows: %w", err)
	}

	var out []skillRow
	// State rows for names that are no longer built-ins are ignored: only the
	// embedded set drives this loop.
	for _, b := range builderBuiltinSkills() {
		r := skillRow{Entry: b.Entry(true), license: b.License, provenance: b.Source, locked: true}
		if st, ok := states[b.Name]; ok {
			r.Enabled = st.enabled
			at, email := st.at, st.email
			r.updatedAt, r.updatedByEmail = &at, &email
		}
		out = append(out, r)
	}

	crows, err := q.Query(ctx,
		`SELECT s.name, s.enabled, v.version, v.description, v.license, v.created_at, COALESCE(u.email, '')
		 FROM zeep_system.builder_skills s
		 JOIN LATERAL (
		   SELECT version, description, license, created_at, created_by
		   FROM zeep_system.builder_skill_versions
		   WHERE skill_id = s.id ORDER BY version DESC LIMIT 1
		 ) v ON true
		 LEFT JOIN zeep_system.dashboard_users u ON u.id = v.created_by
		 WHERE s.deleted_at IS NULL
		 ORDER BY s.name`)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list custom skills: %w", err)
	}
	defer crows.Close()
	for crows.Next() {
		var r skillRow
		var at time.Time
		var email string
		r.Source = skills.OriginCustom
		if err := crows.Scan(&r.Name, &r.Enabled, &r.Version, &r.Description, &r.license, &at, &email); err != nil {
			return nil, fmt.Errorf("dashboard: scan custom skill: %w", err)
		}
		r.updatedAt, r.updatedByEmail = &at, &email
		out = append(out, r)
	}
	if err := crows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard: custom skill rows: %w", err)
	}
	return out, nil
}

func enabledEntries(rows []skillRow) []skills.Entry {
	var out []skills.Entry
	for _, r := range rows {
		if r.Enabled {
			out = append(out, r.Entry)
		}
	}
	return out
}

// ListBuilderSkills returns the merged list (built-ins first, then custom,
// each by name) and the index size against its budget.
func ListBuilderSkills(ctx context.Context, q db.Querier) ([]BuilderSkillSummary, BuilderSkillsIndexInfo, error) {
	rows, err := loadSkillRows(ctx, q)
	if err != nil {
		return nil, BuilderSkillsIndexInfo{}, err
	}
	return summarize(rows)
}

func summarize(rows []skillRow) ([]BuilderSkillSummary, BuilderSkillsIndexInfo, error) {
	entries := enabledEntries(rows)
	_, dropped := skills.RenderIndex(entries, agent.MaxSkillsIndexChars)
	hidden := map[string]bool{}
	for _, d := range dropped {
		hidden[d] = true
	}
	info := BuilderSkillsIndexInfo{
		Chars:   skills.IndexChars(entries),
		Max:     agent.MaxSkillsIndexChars,
		Dropped: append([]string{}, dropped...),
	}

	out := make([]BuilderSkillSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, BuilderSkillSummary{
			Name: r.Name, Description: r.Description, Source: string(r.Source),
			Enabled: r.Enabled, Locked: r.locked, Version: r.Version,
			License: r.license, Provenance: r.provenance,
			InAgentView: r.Enabled && !hidden[r.Name],
			Chars:       skills.EntryChars(r.Entry),
			UpdatedAt:   r.updatedAt, UpdatedByEmail: r.updatedByEmail,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		bi, bj := out[i].Source == string(skills.OriginBuiltin), out[j].Source == string(skills.OriginBuiltin)
		if bi != bj {
			return bi
		}
		return out[i].Name < out[j].Name
	})
	return out, info, nil
}

// GetBuilderSkill returns one skill with its body. version 0 means the latest.
// It returns nil (not an error) when the skill, or the asked version, does not
// exist.
func GetBuilderSkill(ctx context.Context, q db.Querier, name string, version int) (*BuilderSkillDetail, error) {
	rows, err := loadSkillRows(ctx, q)
	if err != nil {
		return nil, err
	}
	sums, _, _ := summarize(rows)
	var sum *BuilderSkillSummary
	for i := range sums {
		if sums[i].Name == name {
			sum = &sums[i]
		}
	}
	if sum == nil {
		return nil, nil
	}
	d := &BuilderSkillDetail{BuilderSkillSummary: *sum}

	if b, ok := builtinSkillByName(name); ok {
		if version != 0 && version != b.Version {
			return nil, nil
		}
		d.Body = b.Body
		return d, nil
	}

	var skillID string
	if err := q.QueryRow(ctx,
		`SELECT id::text FROM zeep_system.builder_skills WHERE name = $1 AND deleted_at IS NULL`, name,
	).Scan(&skillID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("dashboard: get builder skill: %w", err)
	}

	vrows, err := q.Query(ctx,
		`SELECT v.version, v.description, v.license, v.body, v.content_sha256, v.created_at, COALESCE(u.email, '')
		 FROM zeep_system.builder_skill_versions v
		 LEFT JOIN zeep_system.dashboard_users u ON u.id = v.created_by
		 WHERE v.skill_id = $1::uuid ORDER BY v.version DESC`, skillID)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list builder skill versions: %w", err)
	}
	defer vrows.Close()
	found := false
	for vrows.Next() {
		var vi BuilderSkillVersionInfo
		var desc, license, body string
		if err := vrows.Scan(&vi.Version, &desc, &license, &body, &vi.ContentSHA256, &vi.CreatedAt, &vi.CreatedByEmail); err != nil {
			return nil, fmt.Errorf("dashboard: scan builder skill version: %w", err)
		}
		d.Versions = append(d.Versions, vi)
		if (version == 0 && !found) || version == vi.Version {
			found = true
			d.Body, d.Description, d.License, d.Version = body, desc, license, vi.Version
		}
	}
	if err := vrows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard: builder skill version rows: %w", err)
	}
	if !found {
		return nil, nil
	}
	return d, nil
}

// builderSkillContentSHA is the identity of a skill's stored content: what is
// persisted, not the raw upload, so a change to an ignored key or to
// whitespace outside the body is not a new version.
func builderSkillContentSHA(p skills.Parsed) string {
	h := sha256.New()
	for _, part := range []string{p.Name, p.Description, p.License, p.Body} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

const builderSkillsLock = `SELECT pg_advisory_xact_lock(hashtext('zeep_system.builder_skills'))`

// checkSkillsBudget rejects a change whose resulting index is over budget AND
// bigger than the current one. A change that keeps or shrinks an index that is
// already over budget stays allowed, so an operator can always repair it.
func checkSkillsBudget(before, after []skills.Entry) error {
	b, a := skills.IndexChars(before), skills.IndexChars(after)
	if a > agent.MaxSkillsIndexChars && a > b {
		return &BuilderSkillsBudgetError{Chars: a, Max: agent.MaxSkillsIndexChars}
	}
	return nil
}

// withEntry returns entries with e replacing the one of the same name, or
// appended when there is none.
func withEntry(entries []skills.Entry, e skills.Entry) []skills.Entry {
	out := make([]skills.Entry, 0, len(entries)+1)
	replaced := false
	for _, x := range entries {
		if x.Name == e.Name {
			out = append(out, e)
			replaced = true
			continue
		}
		out = append(out, x)
	}
	if !replaced {
		out = append(out, e)
	}
	return out
}

// SaveBuilderSkill stores a validated custom skill: version 1 for a new name,
// version N+1 for a live one, nothing when the content is unchanged. Built-in
// names fail with ErrBuilderSkillBuiltin and other orbit-* names with
// ErrBuilderSkillReserved. A change that overflows the index budget fails with
// *BuilderSkillsBudgetError.
func SaveBuilderSkill(ctx context.Context, pool *db.Pool, authorID string, p skills.Parsed) (*BuilderSkillSaveResult, error) {
	if _, ok := builtinSkillByName(p.Name); ok {
		return nil, ErrBuilderSkillBuiltin
	}
	if skills.HasReservedPrefix(p.Name) {
		return nil, ErrBuilderSkillReserved
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard: save builder skill: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, builderSkillsLock); err != nil {
		return nil, fmt.Errorf("dashboard: save builder skill: lock: %w", err)
	}

	sha := builderSkillContentSHA(p)
	res := &BuilderSkillSaveResult{Name: p.Name, SHA256: sha, Chars: utf8.RuneCountInString(p.Body)}

	var skillID string
	var enabled bool
	err = tx.QueryRow(ctx,
		`SELECT id::text, enabled FROM zeep_system.builder_skills
		 WHERE lower(name) = lower($1) AND deleted_at IS NULL`, p.Name,
	).Scan(&skillID, &enabled)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("dashboard: save builder skill: find: %w", err)
	}

	next := 1
	if exists {
		var latest int
		var latestSHA string
		if err := tx.QueryRow(ctx,
			`SELECT version, content_sha256 FROM zeep_system.builder_skill_versions
			 WHERE skill_id = $1::uuid ORDER BY version DESC LIMIT 1`, skillID,
		).Scan(&latest, &latestSHA); err != nil {
			return nil, fmt.Errorf("dashboard: save builder skill: latest: %w", err)
		}
		if latestSHA == sha {
			res.Version = latest
			return res, nil
		}
		next = latest + 1
	}

	// A disabled skill is not in the index, so its new version cannot grow it.
	if !exists || enabled {
		rows, err := loadSkillRows(ctx, tx)
		if err != nil {
			return nil, err
		}
		before := enabledEntries(rows)
		cand := skills.Entry{Name: p.Name, Description: p.Description, Source: skills.OriginCustom, Version: next, Enabled: true}
		if err := checkSkillsBudget(before, withEntry(before, cand)); err != nil {
			return nil, err
		}
	}

	if !exists {
		if err := tx.QueryRow(ctx,
			`INSERT INTO zeep_system.builder_skills (name, created_by) VALUES ($1, $2::uuid) RETURNING id::text`,
			p.Name, authorID,
		).Scan(&skillID); err != nil {
			return nil, fmt.Errorf("dashboard: save builder skill: insert skill: %w", err)
		}
	}
	// clock_timestamp(), not now(): same reasoning as SaveBuilderPrompt.
	if _, err := tx.Exec(ctx,
		`INSERT INTO zeep_system.builder_skill_versions
		   (skill_id, version, description, license, body, content_sha256, created_by, created_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, $6, $7::uuid, clock_timestamp())`,
		skillID, next, p.Description, p.License, p.Body, sha, authorID,
	); err != nil {
		return nil, fmt.Errorf("dashboard: save builder skill: insert version: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("dashboard: save builder skill: commit: %w", err)
	}
	res.Version, res.Created, res.NewLineage = next, true, next == 1
	return res, nil
}

// SetBuilderSkillEnabled enables or disables a skill, built-in or custom. It
// returns changed=false when the skill already had that state. Enabling a
// skill that would overflow the index fails with *BuilderSkillsBudgetError.
func SetBuilderSkillEnabled(ctx context.Context, pool *db.Pool, userID, name string, enabled bool) (changed bool, err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("dashboard: set builder skill enabled: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, builderSkillsLock); err != nil {
		return false, fmt.Errorf("dashboard: set builder skill enabled: lock: %w", err)
	}

	rows, err := loadSkillRows(ctx, tx)
	if err != nil {
		return false, err
	}
	var target *skillRow
	for i := range rows {
		if rows[i].Name == name {
			target = &rows[i]
		}
	}
	if target == nil {
		return false, ErrBuilderSkillNotFound
	}
	if target.Enabled == enabled {
		return false, nil
	}

	if enabled {
		before := enabledEntries(rows)
		cand := target.Entry
		cand.Enabled = true
		if err := checkSkillsBudget(before, withEntry(before, cand)); err != nil {
			return false, err
		}
	}

	if target.locked {
		_, err = tx.Exec(ctx,
			`INSERT INTO zeep_system.builder_skill_builtin_state (name, enabled, updated_by, updated_at)
			 VALUES ($1, $2, $3::uuid, clock_timestamp())
			 ON CONFLICT (name) DO UPDATE SET enabled = EXCLUDED.enabled, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
			name, enabled, userID)
	} else {
		_, err = tx.Exec(ctx,
			`UPDATE zeep_system.builder_skills SET enabled = $2 WHERE name = $1 AND deleted_at IS NULL`,
			name, enabled)
	}
	if err != nil {
		return false, fmt.Errorf("dashboard: set builder skill enabled: write: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("dashboard: set builder skill enabled: commit: %w", err)
	}
	return true, nil
}

// DeleteBuilderSkill soft-deletes a custom skill. Its versions stay for audit,
// and the name can be uploaded again as a new lineage. Built-ins fail with
// ErrBuilderSkillBuiltin.
func DeleteBuilderSkill(ctx context.Context, pool *db.Pool, userID, name string) error {
	if _, ok := builtinSkillByName(name); ok {
		return ErrBuilderSkillBuiltin
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dashboard: delete builder skill: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, builderSkillsLock); err != nil {
		return fmt.Errorf("dashboard: delete builder skill: lock: %w", err)
	}
	tag, err := tx.Exec(ctx,
		`UPDATE zeep_system.builder_skills SET deleted_at = clock_timestamp(), deleted_by = $2::uuid
		 WHERE name = $1 AND deleted_at IS NULL`, name, userID)
	if err != nil {
		return fmt.Errorf("dashboard: delete builder skill: update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBuilderSkillNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("dashboard: delete builder skill: commit: %w", err)
	}
	return nil
}

// BuilderSkillSource serves the agent: the enabled skills as they are right
// now. It implements skills.Source over the embedded built-ins and Postgres.
type BuilderSkillSource struct {
	q db.Querier
}

var _ skills.Source = (*BuilderSkillSource)(nil)

// NewBuilderSkillSource returns the agent-facing source.
func NewBuilderSkillSource(q db.Querier) *BuilderSkillSource {
	return &BuilderSkillSource{q: q}
}

// Entries lists the enabled skills with descriptions only; bodies are never
// read here, so building the index stays cheap however large the skills are.
func (s *BuilderSkillSource) Entries(ctx context.Context) ([]skills.Entry, error) {
	rows, err := loadSkillRows(ctx, s.q)
	if err != nil {
		return nil, err
	}
	return enabledEntries(rows), nil
}

// Get returns the latest version of an enabled skill, or skills.ErrNotAvailable
// for a name that is unknown, disabled or deleted.
func (s *BuilderSkillSource) Get(ctx context.Context, name string) (skills.Loaded, error) {
	if b, ok := builtinSkillByName(name); ok {
		var enabled bool
		err := s.q.QueryRow(ctx,
			`SELECT COALESCE((SELECT enabled FROM zeep_system.builder_skill_builtin_state WHERE name = $1), true)`, name,
		).Scan(&enabled)
		if err != nil {
			return skills.Loaded{}, fmt.Errorf("dashboard: builtin skill state: %w", err)
		}
		if !enabled {
			return skills.Loaded{}, skills.ErrNotAvailable
		}
		return skills.Loaded{Name: b.Name, Source: skills.OriginBuiltin, Version: b.Version, Body: b.Body}, nil
	}

	var l skills.Loaded
	err := s.q.QueryRow(ctx,
		`SELECT s.name, v.version, v.body
		 FROM zeep_system.builder_skills s
		 JOIN LATERAL (
		   SELECT version, body FROM zeep_system.builder_skill_versions
		   WHERE skill_id = s.id ORDER BY version DESC LIMIT 1
		 ) v ON true
		 WHERE s.name = $1 AND s.deleted_at IS NULL AND s.enabled`, name,
	).Scan(&l.Name, &l.Version, &l.Body)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return skills.Loaded{}, skills.ErrNotAvailable
		}
		return skills.Loaded{}, fmt.Errorf("dashboard: get custom skill: %w", err)
	}
	l.Source = skills.OriginCustom
	return l, nil
}
