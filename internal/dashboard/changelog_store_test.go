package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func TestChangelogCrud(t *testing.T) {
	pool, _, cleanup := db.SetupTestPool(t)
	defer cleanup()

	ctx := context.Background()

	entry := &ChangelogEntry{
		Version:     "1.0.0",
		ReleaseDate: "2026-07-26",
		Title:       "Initial Release",
		Summary:     "First version",
		Sections: `[
			{"type": "features", "items": [{"description": "Added login"}]},
			{"type": "fixes", "items": [{"description": "Fixed sidebar"}]}
		]`,
		Published: true,
	}

	if err := CreateChangelogEntry(ctx, pool, entry); err != nil {
		t.Fatalf("CreateChangelogEntry: %v", err)
	}
	if entry.ID == "" {
		t.Fatal("expected ID to be set")
	}

	entries, total, err := ListChangelogEntries(ctx, pool, 10, 0)
	if err != nil {
		t.Fatalf("ListChangelogEntries: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total 1, got %d", total)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", entries[0].Version)
	}

	entry.Title = "Updated Title"
	if err := UpdateChangelogEntry(ctx, pool, entry); err != nil {
		t.Fatalf("UpdateChangelogEntry: %v", err)
	}

	entries, _, _ = ListChangelogEntries(ctx, pool, 10, 0)
	if entries[0].Title != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got %s", entries[0].Title)
	}

	if err := DeleteChangelogEntry(ctx, pool, entry.ID); err != nil {
		t.Fatalf("DeleteChangelogEntry: %v", err)
	}

	entries, _, _ = ListChangelogEntries(ctx, pool, 10, 0)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after delete, got %d", len(entries))
	}
}

func TestChangelogDeleteNotFound(t *testing.T) {
	pool, _, cleanup := db.SetupTestPool(t)
	defer cleanup()

	err := DeleteChangelogEntry(context.Background(), pool, "00000000-0000-0000-0000-000000000000")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestChangelogPagination(t *testing.T) {
	pool, _, cleanup := db.SetupTestPool(t)
	defer cleanup()

	ctx := context.Background()

	for i := 0; i < 5; i++ {
		entry := &ChangelogEntry{
			Version:     "v" + string(rune('1'+i)),
			ReleaseDate: "2026-07-26",
			Title:       "Release",
			Sections:    "[]",
			Published:   true,
		}
		if err := CreateChangelogEntry(ctx, pool, entry); err != nil {
			t.Fatalf("CreateChangelogEntry %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	entries, total, err := ListChangelogEntries(ctx, pool, 2, 0)
	if err != nil {
		t.Fatalf("ListChangelogEntries: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries (limit), got %d", len(entries))
	}

	entries, _, err = ListChangelogEntries(ctx, pool, 2, 2)
	if err != nil {
		t.Fatalf("ListChangelogEntries page 2: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}
