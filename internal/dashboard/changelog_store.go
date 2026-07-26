package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

type ChangelogEntry struct {
	ID          string    `json:"id"`
	Version     string    `json:"version"`
	ReleaseDate string    `json:"release_date"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	Sections    string    `json:"sections"`
	Published   bool      `json:"published"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func ListChangelogEntries(ctx context.Context, pool *db.Pool, limit, offset int) ([]ChangelogEntry, int, error) {
	var total int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM zeep_system.changelog_entries WHERE published = true`,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("dashboard: count changelog entries: %w", err)
	}

	rows, err := pool.Query(ctx,
		`SELECT id, version, release_date, title, summary, sections, published, created_at, updated_at
		 FROM zeep_system.changelog_entries
		 WHERE published = true
		 ORDER BY release_date DESC, created_at DESC
		 LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("dashboard: list changelog entries: %w", err)
	}
	defer rows.Close()

	var entries []ChangelogEntry
	for rows.Next() {
		var e ChangelogEntry
		var releaseDate time.Time
		if err := rows.Scan(&e.ID, &e.Version, &releaseDate, &e.Title, &e.Summary, &e.Sections, &e.Published, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("dashboard: scan changelog entry: %w", err)
		}
		e.ReleaseDate = releaseDate.Format("2006-01-02")
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []ChangelogEntry{}
	}
	return entries, total, rows.Err()
}

func CreateChangelogEntry(ctx context.Context, pool *db.Pool, entry *ChangelogEntry) error {
	releaseDate, err := time.Parse("2006-01-02", entry.ReleaseDate)
	if err != nil {
		return fmt.Errorf("dashboard: parse release_date: %w", err)
	}

	if entry.Sections == "" {
		entry.Sections = "[]"
	}
	if !json.Valid([]byte(entry.Sections)) {
		return fmt.Errorf("dashboard: invalid sections JSON")
	}

	return pool.QueryRow(ctx,
		`INSERT INTO zeep_system.changelog_entries (version, release_date, title, summary, sections, published, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6, now(), now())
		 RETURNING id, created_at, updated_at`,
		entry.Version, releaseDate, entry.Title, entry.Summary, entry.Sections, entry.Published,
	).Scan(&entry.ID, &entry.CreatedAt, &entry.UpdatedAt)
}

func UpdateChangelogEntry(ctx context.Context, pool *db.Pool, entry *ChangelogEntry) error {
	releaseDate, err := time.Parse("2006-01-02", entry.ReleaseDate)
	if err != nil {
		return fmt.Errorf("dashboard: parse release_date: %w", err)
	}

	if entry.Sections == "" {
		entry.Sections = "[]"
	}
	if !json.Valid([]byte(entry.Sections)) {
		return fmt.Errorf("dashboard: invalid sections JSON")
	}

	tag, err := pool.Exec(ctx,
		`UPDATE zeep_system.changelog_entries
		 SET version = $1, release_date = $2, title = $3, summary = $4, sections = $5::jsonb, published = $6, updated_at = now()
		 WHERE id = $7`,
		entry.Version, releaseDate, entry.Title, entry.Summary, entry.Sections, entry.Published, entry.ID,
	)
	if err != nil {
		return fmt.Errorf("dashboard: update changelog entry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func DeleteChangelogEntry(ctx context.Context, pool *db.Pool, id string) error {
	tag, err := pool.Exec(ctx,
		`DELETE FROM zeep_system.changelog_entries WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("dashboard: delete changelog entry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
