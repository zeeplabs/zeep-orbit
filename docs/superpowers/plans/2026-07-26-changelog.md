# Changelog Feature Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In-app changelog page where superadmin manages versioned release entries with categorized sections. Users see a timeline with load-more pagination. Sidebar link positioned above the user section.

**Architecture:** New `changelog_entries` table in `zeep_system` schema (idempotent DDL). Store provides CRUD (List/Create/Update/Delete) following existing `_store.go` patterns. Handler with superadmin-gated write, any-logged-in read. React page at `/changelog` route, sidebar link with Megaphone icon.

**Tech Stack:** PostgreSQL (pgx), chi router, React + TypeScript, shadcn/ui, sonner, framer-motion, react-i18next

## Global Constraints

- Superadmin-only for write endpoints (Create/Update/Delete); any logged-in user for GET
- Sidebar link positioned between nav items and user section (above the `borderTop` divider)
- GET filters `WHERE published = true` for all users
- Retroactive insertion supported regardless of method (dashboard modal or raw SQL)
- DDL idempotent (`CREATE TABLE IF NOT EXISTS`) in provisioner.go
- Follow existing code patterns: store uses `*db.Pool` + `context.Context`, handler uses `UserFromContext`, `writeJSON`, `ErrNotFound`
- No placeholder code — every step contains complete implementation

---

### Task 1: Add changelog_entries DDL to provisioner

**Files:**
- Modify: `internal/dashboard/provisioner.go` (add one entry to `stmts` slice)

**Interfaces:**
- Produces: table `zeep_system.changelog_entries` with columns: id, version, release_date, title, summary, sections (JSONB), published, created_at, updated_at

- [ ] **Step 1: Add DDL statement**

Open `internal/dashboard/provisioner.go`. After the last ALTER TABLE statement (after line 203), add this entry to the `stmts` slice:

```go
`CREATE TABLE IF NOT EXISTS zeep_system.changelog_entries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version      TEXT NOT NULL,
    release_date DATE NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    sections     JSONB NOT NULL DEFAULT '[]',
    published    BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
)`,
```

- [ ] **Step 2: Build and verify**

```bash
rtk go build ./...
```
Expected: builds without errors.

- [ ] **Step 3: Commit**

```bash
rtk git add internal/dashboard/provisioner.go && rtk git commit -m "feat(changelog): add changelog_entries DDL"
```

---

### Task 2: Create changelog store

**Files:**
- Create: `internal/dashboard/changelog_store.go`

**Interfaces:**
- Produces:
  - `type ChangelogEntry struct { ... }` — exported struct with json tags
  - `type ChangelogSectionItem struct { Description string "json:\"description\"" }` — unexported
  - `type ChangelogSection struct { Type string "json:\"type\""; Items []ChangelogSectionItem "json:\"items\"" }` — unexported
  - `func ListChangelogEntries(ctx context.Context, pool *db.Pool, limit, offset int) ([]ChangelogEntry, int, error)` — returns entries + total count
  - `func CreateChangelogEntry(ctx context.Context, pool *db.Pool, entry *ChangelogEntry) error` — sets ID and timestamps
  - `func UpdateChangelogEntry(ctx context.Context, pool *db.Pool, entry *ChangelogEntry) error` — returns ErrNotFound if no rows
  - `func DeleteChangelogEntry(ctx context.Context, pool *db.Pool, id string) error` — returns ErrNotFound if no rows

- [ ] **Step 1: Write the store test**

Create `internal/dashboard/changelog_store_test.go`:

```go
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
```

- [ ] **Step 2: Run tests and verify they fail**

```bash
rtk go test ./internal/dashboard/ -run TestChangelog -v -count=1
```
Expected: compilation errors (types not defined yet).

- [ ] **Step 3: Write the store implementation**

Create `internal/dashboard/changelog_store.go`:

```go
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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
```

- [ ] **Step 4: Run the tests**

```bash
rtk go test ./internal/dashboard/ -run TestChangelog -v -count=1
```
Expected: all 3 tests pass.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/dashboard/changelog_store.go internal/dashboard/changelog_store_test.go && rtk git commit -m "feat(changelog): add store with CRUD"
```

---

### Task 3: Create changelog HTTP handler

**Files:**
- Create: `internal/dashboard/changelog.go`

**Interfaces:**
- Consumes: `ListChangelogEntries`, `CreateChangelogEntry`, `UpdateChangelogEntry`, `DeleteChangelogEntry` from Task 2
- Produces: `type ChangelogHandler struct { pool *db.Pool }`, `func NewChangelogHandler(pool *db.Pool) *ChangelogHandler`, methods: `List`, `Create`, `Update`, `Delete`

- [ ] **Step 1: Write the handler**

Create `internal/dashboard/changelog.go`:

```go
package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

type ChangelogHandler struct {
	pool *db.Pool
}

func NewChangelogHandler(pool *db.Pool) *ChangelogHandler {
	return &ChangelogHandler{pool: pool}
}

func (h *ChangelogHandler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	_ = user

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	entries, total, err := ListChangelogEntries(r.Context(), h.pool, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"entries": entries,
		"total":   total,
	})
}

type changelogEntryBody struct {
	Version     string `json:"version"`
	ReleaseDate string `json:"release_date"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Sections    string `json:"sections"`
	Published   *bool  `json:"published"`
}

func (h *ChangelogHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	var body changelogEntryBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.Version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "version is required"})
		return
	}
	if body.ReleaseDate == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "release_date is required"})
		return
	}

	published := true
	if body.Published != nil {
		published = *body.Published
	}

	entry := &ChangelogEntry{
		Version:     body.Version,
		ReleaseDate: body.ReleaseDate,
		Title:       body.Title,
		Summary:     body.Summary,
		Sections:    body.Sections,
		Published:   published,
	}

	if err := CreateChangelogEntry(r.Context(), h.pool, entry); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	meta, _ := json.Marshal(map[string]string{"version": entry.Version})
	_ = InsertAuditLog(r.Context(), h.pool, user.ID, user.Email, "changelog.create", "changelog_entry", entry.ID, entry.Version, meta, r.RemoteAddr)

	writeJSON(w, http.StatusCreated, entry)
}

func (h *ChangelogHandler) Update(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	var body changelogEntryBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.Version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "version is required"})
		return
	}
	if body.ReleaseDate == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "release_date is required"})
		return
	}

	published := true
	if body.Published != nil {
		published = *body.Published
	}

	entry := &ChangelogEntry{
		ID:          id,
		Version:     body.Version,
		ReleaseDate: body.ReleaseDate,
		Title:       body.Title,
		Summary:     body.Summary,
		Sections:    body.Sections,
		Published:   published,
	}

	if err := UpdateChangelogEntry(r.Context(), h.pool, entry); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "entry not found"})
		return
	}

	meta, _ := json.Marshal(map[string]string{"version": entry.Version})
	_ = InsertAuditLog(r.Context(), h.pool, user.ID, user.Email, "changelog.update", "changelog_entry", id, entry.Version, meta, r.RemoteAddr)

	writeJSON(w, http.StatusOK, entry)
}

func (h *ChangelogHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}

	if err := DeleteChangelogEntry(r.Context(), h.pool, id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "entry not found"})
		return
	}

	meta, _ := json.Marshal(map[string]string{"id": id})
	_ = InsertAuditLog(r.Context(), h.pool, user.ID, user.Email, "changelog.delete", "changelog_entry", id, "", meta, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
```

- [ ] **Step 2: Build**

```bash
rtk go build ./...
```
Expected: builds without errors.

- [ ] **Step 3: Commit**

```bash
rtk git add internal/dashboard/changelog.go && rtk git commit -m "feat(changelog): add HTTP handler"
```

---

### Task 4: Register changelog routes in server

**Files:**
- Modify: `internal/server/server.go:55-69` (handler creation), `:132` (function signature), `:218-233` (route registration)

**Interfaces:**
- Consumes: `*ChangelogHandler` from Task 3

- [ ] **Step 1: Add handler creation and wire up routes**

In `internal/server/server.go`, make these changes:

**A. Add changelog handler creation (after line 68):**

```go
	changelogH := dashboard.NewChangelogHandler(pool)
```

**B. Update newRouter signature (line 132) — add `changelogH *dashboard.ChangelogHandler` to parameters.**

The old signature:
```go
func newRouter(reg *registry.Registry, h *Handler, pool *db.Pool, logger *zap.Logger, dashH *dashboard.Handler, githubConfigH *dashboard.GitHubConfigHandler, githubTemplatesH *dashboard.GitHubTemplatesHandler, frontendAppsH *dashboard.FrontendAppsHandler, deployProviderH *dashboard.DeployProviderConfigHandler) *chi.Mux {
```

The new signature:
```go
func newRouter(reg *registry.Registry, h *Handler, pool *db.Pool, logger *zap.Logger, dashH *dashboard.Handler, githubConfigH *dashboard.GitHubConfigHandler, githubTemplatesH *dashboard.GitHubTemplatesHandler, frontendAppsH *dashboard.FrontendAppsHandler, deployProviderH *dashboard.DeployProviderConfigHandler, changelogH *dashboard.ChangelogHandler) *chi.Mux {
```

**C. Update the call to newRouter (line 69):**

```go
	r := newRouter(reg, h, pool, logger, dashH, githubConfigH, githubTemplatesH, frontendAppsH, deployProviderH, changelogH)
```

**D. Register routes (after line 232, before the StaticHandler line):**

After:
```go
		r.With(dashboard.RequireAuth(pool)).Put("/api/deploy-provider/config", deployProviderH.UpdateFields)
```

Add:
```go
		r.With(dashboard.RequireAuth(pool)).Get("/api/changelog", changelogH.List)
		r.With(dashboard.RequireAuth(pool)).Post("/api/changelog", changelogH.Create)
		r.With(dashboard.RequireAuth(pool)).Put("/api/changelog/{id}", changelogH.Update)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/changelog/{id}", changelogH.Delete)
```

- [ ] **Step 2: Build and verify**

```bash
rtk go build ./...
```
Expected: builds without errors.

- [ ] **Step 3: Commit**

```bash
rtk git add internal/server/server.go && rtk git commit -m "feat(changelog): register routes in server"
```

---

### Task 5: Add i18n strings

**Files:**
- Modify: `internal/dashboard/ui/src/locales/pt-BR.json`
- Modify: `internal/dashboard/ui/src/locales/en.json`

**Interfaces:**
- Produces: i18n keys: `nav.changelog`, `changelog.title`, `changelog.empty`, `changelog.loadMore`, `changelog.add`, `changelog.edit`, `changelog.delete`, `changelog.version`, `changelog.date`, `changelog.titlePlaceholder`, `changelog.summaryPlaceholder`, `changelog.sectionType`, `changelog.sectionDesc`, `changelog.itemDesc`, `changelog.addSection`, `changelog.addItem`, `changelog.save`, `changelog.cancel`, `changelog.sectionType.*` (features, improvements, fixes, security, breaking)

- [ ] **Step 1: Add strings to pt-BR.json**

In `internal/dashboard/ui/src/locales/pt-BR.json`, add after the last nav key (after `"nav.loggingOut": "Saindo..."`, line ~20):

```json
  "nav.changelog": "Changelog",
```

Add at the end of the file (before the closing `}`):

```json
  "changelog.title": "Changelog",
  "changelog.empty": "Nenhuma atualização publicada ainda.",
  "changelog.loadMore": "Carregar mais",
  "changelog.add": "Adicionar entrada",
  "changelog.edit": "Editar entrada",
  "changelog.delete": "Remover entrada",
  "changelog.deleteConfirm": "Remover esta entrada do changelog?",
  "changelog.deleteDesc": "Esta ação não pode ser desfeita.",
  "changelog.version": "Versão",
  "changelog.date": "Data",
  "changelog.titleField": "Título (opcional)",
  "changelog.summaryField": "Resumo (opcional)",
  "changelog.sectionType": "Tipo da seção",
  "changelog.sectionDesc": "Descrição da seção",
  "changelog.itemDesc": "Descrição do item",
  "changelog.addSection": "Adicionar seção",
  "changelog.addItem": "Adicionar item",
  "changelog.createTitle": "Nova entrada no changelog",
  "changelog.editTitle": "Editar entrada",
  "changelog.create": "Criar",
  "changelog.save": "Salvar",
  "changelog.cancel": "Cancelar",
  "changelog.published": "Publicado",
  "changelog.sectionType.features": "Novidades",
  "changelog.sectionType.improvements": "Melhorias",
  "changelog.sectionType.fixes": "Correções",
  "changelog.sectionType.security": "Segurança",
  "changelog.sectionType.breaking": "Breaking Changes"
```

- [ ] **Step 2: Add strings to en.json**

In `internal/dashboard/ui/src/locales/en.json`, add after the last nav key (after `"nav.loggingOut": "Logging out..."`, line ~20):

```json
  "nav.changelog": "Changelog",
```

Add at the end of the file (before the closing `}`):

```json
  "changelog.title": "Changelog",
  "changelog.empty": "No updates published yet.",
  "changelog.loadMore": "Load more",
  "changelog.add": "Add entry",
  "changelog.edit": "Edit entry",
  "changelog.delete": "Delete entry",
  "changelog.deleteConfirm": "Remove this changelog entry?",
  "changelog.deleteDesc": "This action cannot be undone.",
  "changelog.version": "Version",
  "changelog.date": "Date",
  "changelog.titleField": "Title (optional)",
  "changelog.summaryField": "Summary (optional)",
  "changelog.sectionType": "Section type",
  "changelog.sectionDesc": "Section description",
  "changelog.itemDesc": "Item description",
  "changelog.addSection": "Add section",
  "changelog.addItem": "Add item",
  "changelog.createTitle": "New changelog entry",
  "changelog.editTitle": "Edit entry",
  "changelog.create": "Create",
  "changelog.save": "Save",
  "changelog.cancel": "Cancel",
  "changelog.published": "Published",
  "changelog.sectionType.features": "Features",
  "changelog.sectionType.improvements": "Improvements",
  "changelog.sectionType.fixes": "Fixes",
  "changelog.sectionType.security": "Security",
  "changelog.sectionType.breaking": "Breaking Changes"
```

- [ ] **Step 3: Verify JSON is valid**

```bash
rtk python3 -m json.tool internal/dashboard/ui/src/locales/pt-BR.json > /dev/null && echo "pt-BR.json valid" && rtk python3 -m json.tool internal/dashboard/ui/src/locales/en.json > /dev/null && echo "en.json valid"
```
Expected: both `valid`.

- [ ] **Step 4: Commit**

```bash
rtk git add internal/dashboard/ui/src/locales/pt-BR.json internal/dashboard/ui/src/locales/en.json && rtk git commit -m "feat(changelog): add i18n strings"
```

---

### Task 6: Create ChangelogPage component

**Files:**
- Create: `internal/dashboard/ui/src/pages/ChangelogPage.tsx`

**Interfaces:**
- Consumes: i18n keys from Task 5, `/api/changelog` endpoints from Task 4

- [ ] **Step 1: Write the page component**

Create `internal/dashboard/ui/src/pages/ChangelogPage.tsx`:

```tsx
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Megaphone, Pencil, Trash2, Plus, X } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { DeleteConfirmDialog } from "@/components/DeleteConfirmDialog";

const PAGE_SIZE = 10;

type SectionType = "features" | "improvements" | "fixes" | "security" | "breaking";

interface SectionItem {
  description: string;
}

interface Section {
  type: SectionType;
  items: SectionItem[];
}

interface ChangelogEntry {
  id: string;
  version: string;
  release_date: string;
  title: string;
  summary: string;
  sections: string;
  published: boolean;
  created_at: string;
}

function parseSections(raw: string): Section[] {
  try {
    return JSON.parse(raw);
  } catch {
    return [];
  }
}

const sectionColors: Record<SectionType, { bg: string; text: string; border: string }> = {
  features: { bg: "bg-emerald-500/10", text: "text-emerald-400", border: "border-emerald-500/20" },
  improvements: { bg: "bg-blue-500/10", text: "text-blue-400", border: "border-blue-500/20" },
  fixes: { bg: "bg-amber-500/10", text: "text-amber-400", border: "border-amber-500/20" },
  security: { bg: "bg-red-500/10", text: "text-red-400", border: "border-red-500/20" },
  breaking: { bg: "bg-purple-500/10", text: "text-purple-400", border: "border-purple-500/20" },
};

function formatDate(dateStr: string): string {
  const d = new Date(dateStr + "T00:00:00");
  return d.toLocaleDateString("pt-BR", { day: "2-digit", month: "short", year: "numeric" });
}

function ChangelogEntryView({
  entry,
  isSuperadmin,
  onEdit,
  onDelete,
}: {
  entry: ChangelogEntry;
  isSuperadmin: boolean;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { t } = useTranslation();
  const sections = parseSections(entry.sections);

  return (
    <div className="relative border border-white/[0.06] rounded-xl bg-white/[0.02] p-6">
      {isSuperadmin && (
        <div className="absolute top-4 right-4 flex gap-1">
          <button
            onClick={onEdit}
            className="p-1.5 rounded-lg hover:bg-white/[0.08] text-white/40 hover:text-white/70 transition-colors"
          >
            <Pencil size={14} />
          </button>
          <button
            onClick={onDelete}
            className="p-1.5 rounded-lg hover:bg-red-500/10 text-white/40 hover:text-red-400 transition-colors"
          >
            <Trash2 size={14} />
          </button>
        </div>
      )}

      <div className="flex items-center gap-3 mb-4">
        <span className="px-2.5 py-0.5 rounded-md bg-white/[0.06] text-xs font-mono text-[#F8FAFC]">
          {entry.version}
        </span>
        <span className="text-xs text-[#94A3B8]">{formatDate(entry.release_date)}</span>
        {!entry.published && (
          <span className="text-[10px] px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-400">
            Draft
          </span>
        )}
      </div>

      {entry.title && (
        <h3 className="text-sm font-semibold text-[#F8FAFC] mb-1">{entry.title}</h3>
      )}
      {entry.summary && (
        <p className="text-[13px] text-[#94A3B8] mb-4">{entry.summary}</p>
      )}

      <div className="space-y-4">
        {sections.map((section, si) => {
          const colors = sectionColors[section.type] || sectionColors.fixes;
          return (
            <div key={si}>
              <span
                className={`inline-block px-2 py-0.5 rounded text-[11px] font-semibold ${colors.bg} ${colors.text} border ${colors.border} mb-2`}
              >
                {t(`changelog.sectionType.${section.type}` as any)}
              </span>
              <ul className="space-y-1.5">
                {section.items.map((item, ii) => (
                  <li key={ii} className="flex items-start gap-2 text-[13px] text-[#94A3B8]">
                    <span className="text-white/20 mt-1.5 block w-1 h-1 rounded-full bg-current flex-shrink-0" />
                    {item.description}
                  </li>
                ))}
              </ul>
            </div>
          );
        })}
      </div>
    </div>
  );
}

interface ChangelogFormData {
  id?: string;
  version: string;
  release_date: string;
  title: string;
  summary: string;
  sections: Section[];
  published: boolean;
}

function emptySection(): Section {
  return { type: "features", items: [{ description: "" }] };
}

function ChangelogFormModal({
  open,
  onClose,
  initial,
}: {
  open: boolean;
  onClose: () => void;
  initial?: ChangelogEntry;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const isEdit = !!initial;

  const [form, setForm] = useState<ChangelogFormData>(() => {
    if (initial) {
      return {
        id: initial.id,
        version: initial.version,
        release_date: initial.release_date,
        title: initial.title,
        summary: initial.summary,
        sections: parseSections(initial.sections),
        published: initial.published,
      };
    }
    return {
      version: "",
      release_date: new Date().toISOString().slice(0, 10),
      title: "",
      summary: "",
      sections: [emptySection()],
      published: true,
    };
  });

  const mutation = useMutation({
    mutationFn: async () => {
      const method = isEdit ? "PUT" : "POST";
      const url = isEdit ? `/dashboard/api/changelog/${form.id}` : "/dashboard/api/changelog";
      const res = await fetch(url, {
        method,
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          version: form.version,
          release_date: form.release_date,
          title: form.title,
          summary: form.summary,
          sections: JSON.stringify(form.sections),
          published: form.published,
        }),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        throw new Error((data as any).error || "Failed to save");
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["changelog"] });
      toast.success(isEdit ? "Entrada atualizada!" : "Entrada criada!");
      onClose();
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });

  const updateSection = (si: number, field: keyof Section, value: string) => {
    setForm((prev) => {
      const next = { ...prev, sections: [...prev.sections] };
      if (field === "type") {
        next.sections[si] = { ...next.sections[si], type: value as SectionType };
      }
      return next;
    });
  };

  const updateItem = (si: number, ii: number, value: string) => {
    setForm((prev) => {
      const next = { ...prev, sections: [...prev.sections] };
      const items = [...next.sections[si].items];
      items[ii] = { description: value };
      next.sections[si] = { ...next.sections[si], items };
      return next;
    });
  };

  const addSection = () => {
    setForm((prev) => ({
      ...prev,
      sections: [...prev.sections, emptySection()],
    }));
  };

  const removeSection = (si: number) => {
    setForm((prev) => ({
      ...prev,
      sections: prev.sections.filter((_, i) => i !== si),
    }));
  };

  const addItem = (si: number) => {
    setForm((prev) => {
      const next = { ...prev, sections: [...prev.sections] };
      next.sections[si] = {
        ...next.sections[si],
        items: [...next.sections[si].items, { description: "" }],
      };
      return next;
    });
  };

  const removeItem = (si: number, ii: number) => {
    setForm((prev) => {
      const next = { ...prev, sections: [...prev.sections] };
      next.sections[si] = {
        ...next.sections[si],
        items: next.sections[si].items.filter((_, i) => i !== ii),
      };
      return next;
    });
  };

  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent className="max-w-xl max-h-[85vh] overflow-y-auto border border-white/[0.10] bg-[#0D0D14]/60 backdrop-blur-xl rounded-2xl p-0 gap-0">
        <div className="bg-white/[0.04] shadow-[inset_0_1px_1px_rgba(255,255,255,0.10)] rounded-[calc(1rem-2px)] px-7 pb-6 pt-7">
          <DialogHeader className="mb-4">
            <DialogTitle className="text-base font-bold text-[#F8FAFC]">
              {isEdit ? t("changelog.editTitle") : t("changelog.createTitle")}
            </DialogTitle>
            <DialogDescription className="text-[13px] text-[#94A3B8] mt-1" />
          </DialogHeader>

          <div className="space-y-3">
            <div>
              <label className="text-[11px] font-semibold text-[#94A3B8] uppercase tracking-wide">
                {t("changelog.version")}
              </label>
              <Input
                value={form.version}
                onChange={(e) => setForm((p) => ({ ...p, version: e.target.value }))}
                placeholder="1.0.0"
                className="mt-1"
              />
            </div>
            <div>
              <label className="text-[11px] font-semibold text-[#94A3B8] uppercase tracking-wide">
                {t("changelog.date")}
              </label>
              <Input
                type="date"
                value={form.release_date}
                onChange={(e) => setForm((p) => ({ ...p, release_date: e.target.value }))}
                className="mt-1"
              />
            </div>
            <div>
              <label className="text-[11px] font-semibold text-[#94A3B8] uppercase tracking-wide">
                {t("changelog.titleField")}
              </label>
              <Input
                value={form.title}
                onChange={(e) => setForm((p) => ({ ...p, title: e.target.value }))}
                placeholder="Initial Release"
                className="mt-1"
              />
            </div>
            <div>
              <label className="text-[11px] font-semibold text-[#94A3B8] uppercase tracking-wide">
                {t("changelog.summaryField")}
              </label>
              <Input
                value={form.summary}
                onChange={(e) => setForm((p) => ({ ...p, summary: e.target.value }))}
                placeholder="Summary..."
                className="mt-1"
              />
            </div>

            <div>
              <div className="flex items-center justify-between mb-2">
                <label className="text-[11px] font-semibold text-[#94A3B8] uppercase tracking-wide">
                  Sections
                </label>
                <button
                  type="button"
                  onClick={addSection}
                  className="text-[11px] text-[--brand-primary] hover:underline"
                >
                  + {t("changelog.addSection")}
                </button>
              </div>
              <div className="space-y-3">
                {form.sections.map((section, si) => (
                  <div key={si} className="p-3 rounded-lg border border-white/[0.06] bg-white/[0.02]">
                    <div className="flex items-center gap-2 mb-2">
                      <select
                        value={section.type}
                        onChange={(e) => updateSection(si, "type", e.target.value)}
                        className="flex-1 rounded-lg border border-white/[0.10] bg-white/[0.04] text-[#F8FAFC] text-xs px-2 py-1.5"
                      >
                        <option value="features">{t("changelog.sectionType.features" as any)}</option>
                        <option value="improvements">{t("changelog.sectionType.improvements" as any)}</option>
                        <option value="fixes">{t("changelog.sectionType.fixes" as any)}</option>
                        <option value="security">{t("changelog.sectionType.security" as any)}</option>
                        <option value="breaking">{t("changelog.sectionType.breaking" as any)}</option>
                      </select>
                      <button
                        type="button"
                        onClick={() => removeSection(si)}
                        className="p-1 rounded text-white/30 hover:text-red-400"
                      >
                        <X size={14} />
                      </button>
                    </div>
                    <div className="space-y-1.5">
                      {section.items.map((item, ii) => (
                        <div key={ii} className="flex items-center gap-1.5">
                          <Input
                            value={item.description}
                            onChange={(e) => updateItem(si, ii, e.target.value)}
                            placeholder={t("changelog.itemDesc")}
                            className="flex-1 h-8 text-xs"
                          />
                          <button
                            type="button"
                            onClick={() => removeItem(si, ii)}
                            className="p-1 rounded text-white/30 hover:text-red-400 flex-shrink-0"
                          >
                            <X size={12} />
                          </button>
                        </div>
                      ))}
                    </div>
                    <button
                      type="button"
                      onClick={() => addItem(si)}
                      className="mt-2 text-[10px] text-white/40 hover:text-white/70"
                    >
                      + {t("changelog.addItem")}
                    </button>
                  </div>
                ))}
              </div>
            </div>
          </div>

          <DialogFooter className="flex flex-row gap-2.5 sm:flex-row sm:justify-end sm:space-x-0 mt-4">
            <Button
              variant="outline"
              onClick={onClose}
              className="rounded-xl border-white/[0.10] bg-white/[0.06] text-[#94A3B8] hover:bg-white/[0.10]"
            >
              {t("changelog.cancel")}
            </Button>
            <Button
              onClick={() => mutation.mutate()}
              disabled={mutation.isPending || !form.version}
              className="rounded-xl border-0 text-white font-semibold disabled:opacity-40"
              style={{
                background: "linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))",
              }}
            >
              {mutation.isPending ? "..." : isEdit ? t("changelog.save") : t("changelog.create")}
            </Button>
          </DialogFooter>
        </div>
      </DialogContent>
    </Dialog>
  );
}

export default function ChangelogPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [offset, setOffset] = useState(0);
  const [showForm, setShowForm] = useState(false);
  const [editingEntry, setEditingEntry] = useState<ChangelogEntry | undefined>();
  const [deletingEntry, setDeletingEntry] = useState<ChangelogEntry | undefined>();

  const { data: me } = useQuery({
    queryKey: ["me"],
    queryFn: async () => {
      const res = await fetch("/dashboard/api/me", { credentials: "include" });
      return res.json();
    },
    staleTime: 60000,
  });

  const { data, isLoading } = useQuery({
    queryKey: ["changelog", offset],
    queryFn: async () => {
      const res = await fetch(
        `/dashboard/api/changelog?limit=${PAGE_SIZE}&offset=${offset}`,
        { credentials: "include" }
      );
      if (!res.ok) throw new Error("Failed to load");
      return res.json() as Promise<{ entries: ChangelogEntry[]; total: number }>;
    },
  });

  const deleteMutation = useMutation({
    mutationFn: async (id: string) => {
      const res = await fetch(`/dashboard/api/changelog/${id}`, {
        method: "DELETE",
        credentials: "include",
      });
      if (!res.ok) throw new Error("Failed to delete");
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["changelog"] });
      toast.success("Entrada removida!");
      setDeletingEntry(undefined);
    },
    onError: () => toast.error("Erro ao remover entrada"),
  });

  const isSuperadmin = me?.role === "superadmin";
  const entries = data?.entries || [];
  const total = data?.total || 0;
  const hasMore = offset + PAGE_SIZE < total;

  return (
    <div className="w-full max-w-2xl mx-auto py-2">
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-xl font-bold text-[#F8FAFC]">{t("changelog.title")}</h1>
        </div>
        {isSuperadmin && (
          <Button
            onClick={() => {
              setEditingEntry(undefined);
              setShowForm(true);
            }}
            className="rounded-xl border-0 text-white font-semibold"
            style={{
              background: "linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))",
            }}
            size="sm"
          >
            <Plus size={14} strokeWidth={2} className="mr-1.5" />
            {t("changelog.add")}
          </Button>
        )}
      </div>

      {isLoading ? (
        <div className="text-center text-[#94A3B8] py-12">Carregando...</div>
      ) : entries.length === 0 ? (
        <div className="text-center py-16">
          <Megaphone size={40} strokeWidth={1} className="mx-auto text-white/10 mb-4" />
          <p className="text-[#94A3B8] text-sm">{t("changelog.empty")}</p>
        </div>
      ) : (
        <>
          <div className="space-y-4">
            {entries.map((entry) => (
              <ChangelogEntryView
                key={entry.id}
                entry={entry}
                isSuperadmin={isSuperadmin}
                onEdit={() => {
                  setEditingEntry(entry);
                  setShowForm(true);
                }}
                onDelete={() => setDeletingEntry(entry)}
              />
            ))}
          </div>

          {hasMore && (
            <div className="text-center mt-6">
              <Button
                variant="outline"
                onClick={() => setOffset((p) => p + PAGE_SIZE)}
                className="rounded-xl border-white/[0.10] bg-white/[0.04] text-[#94A3B8] hover:bg-white/[0.08] hover:text-[#F8FAFC]"
              >
                {t("changelog.loadMore")}
              </Button>
            </div>
          )}
        </>
      )}

      <ChangelogFormModal
        open={showForm}
        onClose={() => {
          setShowForm(false);
          setEditingEntry(undefined);
        }}
        initial={editingEntry}
      />

      <DeleteConfirmDialog
        open={!!deletingEntry}
        onClose={() => setDeletingEntry(undefined)}
        onConfirm={() => deletingEntry && deleteMutation.mutate(deletingEntry.id)}
        title={t("changelog.deleteConfirm")}
        description={t("changelog.deleteDesc")}
        loading={deleteMutation.isPending}
      />
    </div>
  );
}
```

- [ ] **Step 2: Build dashboard**

```bash
rtk make dashboard-build
```
Expected: builds successfully.

- [ ] **Step 3: Commit**

```bash
rtk git add internal/dashboard/ui/src/pages/ChangelogPage.tsx && rtk git commit -m "feat(changelog): add ChangelogPage component"
```

---

### Task 7: Add route and sidebar link

**Files:**
- Modify: `internal/dashboard/ui/src/App.tsx` — add page import and route
- Modify: `internal/dashboard/ui/src/pages/DashboardShell.tsx` — add Megaphone icon import, add changelog nav item

- [ ] **Step 1: Add route in App.tsx**

In `internal/dashboard/ui/src/App.tsx`:

**A. Add import (after line 17, after SdkPage):**

```tsx
import ChangelogPage from './pages/ChangelogPage'
```

**B. Add route (after line 119, after the SDK route):**

```tsx
          <Route path="/changelog" element={<ChangelogPage />} />
```

- [ ] **Step 2: Add sidebar link in DashboardShell.tsx**

In `internal/dashboard/ui/src/pages/DashboardShell.tsx`:

**A. Add `Megaphone` to icon imports (line 5-18, add to lucide-react import):**

Change line 7 from:
```tsx
  Lock,
```
to:
```tsx
  Lock,
  Megaphone,
```

**B. Insert changelog link between the nav items `<nav>` section and the user `<div>`.**

After the closing `</nav>` tag (line 327), before `{/* User */}` (line 329), add:

```tsx

        {/* Changelog */}
        <NavLink
          to="/changelog"
          style={({ isActive }) => ({
            display: "flex",
            alignItems: "center",
            gap: 10,
            padding: "9px 12px",
            borderRadius: 10,
            border: "none",
            background: isActive
              ? "rgba(var(--brand-primary-rgb), 0.12)"
              : "transparent",
            color: isActive ? "var(--text)" : "var(--text-muted)",
            cursor: "pointer",
            fontSize: 14,
            textAlign: "left" as const,
            width: "100%",
            fontFamily: "inherit",
            fontWeight: isActive ? 600 : 400,
            position: "relative" as const,
            textDecoration: "none",
            transition: "background 0.15s, color 0.15s",
            marginBottom: 12,
          })}
        >
          {({ isActive }) => (
            <>
              {isActive && (
                <motion.div
                  layoutId="nav-active-indicator"
                  style={{
                    position: "absolute",
                    left: 0,
                    top: "20%",
                    bottom: "20%",
                    width: 3,
                    borderRadius: 2,
                    background: "var(--accent)",
                  }}
                  transition={{ duration: 0.3, ease: [0.32, 0.72, 0, 1] }}
                />
              )}
              <Megaphone size={15} strokeWidth={1.5} />
              {t("nav.changelog")}
            </>
          )}
        </NavLink>
```

- [ ] **Step 3: Build dashboard**

```bash
rtk make dashboard-build
```
Expected: builds successfully.

- [ ] **Step 4: Commit**

```bash
rtk git add internal/dashboard/ui/src/App.tsx internal/dashboard/ui/src/pages/DashboardShell.tsx && rtk git commit -m "feat(changelog): add route and sidebar link"
```

---

### Task 8: Add changelog to mobile bottom bar

**Files:**
- Modify: `internal/dashboard/ui/src/pages/DashboardShell.tsx` — add changelog to `BottomBar` component

- [ ] **Step 1: Add changelog to mobile BottomBar**

In the `BottomBar` component (lines 63-135), inside the JSX that maps over `items`, the `items` array comes from `navItems()`. Since `navItems()` doesn't include the changelog, we need to add it directly in the `BottomBar` JSX.

After the `{items.map(...)}` block (line 111), before the user button (line 112), add:

```tsx
      <NavLink
        to="/changelog"
        className="flex flex-col items-center justify-center flex-1 no-underline"
        style={({ isActive }) => ({
          gap: 2,
          padding: "4px 8px",
          color: isActive ? "var(--brand-primary)" : "var(--text-muted)",
          fontSize: 10,
          fontWeight: isActive ? 600 : 400,
          transition: "color 0.15s",
        })}
      >
        {({ isActive }) => (
          <>
            <Megaphone size={21} strokeWidth={isActive ? 2 : 1.5} />
            <span style={{ fontSize: 10, lineHeight: 1, whiteSpace: "nowrap" }}>
              {t("nav.changelog")}
            </span>
          </>
        )}
      </NavLink>
```

- [ ] **Step 2: Build dashboard**

```bash
rtk make dashboard-build
```
Expected: builds successfully.

- [ ] **Step 3: Commit**

```bash
rtk git add internal/dashboard/ui/src/pages/DashboardShell.tsx && rtk git commit -m "feat(changelog): add to mobile bottom bar"
```

---

### Task 9: Verify E2E

- [ ] **Step 1: Run all Go tests**

```bash
rtk go test ./internal/dashboard/... -count=1 2>&1 | tail -5
```
Expected: all tests pass, including the new changelog store tests.

- [ ] **Step 2: Start server and verify API**

```bash
rtk make run &
sleep 3
# Test changelog list endpoint (requires login session, but server must start)
curl -s http://localhost:8081/dashboard/api/changelog 2>&1 | head -20
```
Expected: server starts, API endpoint responds (will get 401 without session, which is expected).

- [ ] **Step 3: Build dashboard**

```bash
rtk make dashboard-build
```
Expected: builds without errors.

- [ ] **Step 4: Final commit if any changes**

```bash
rtk git status
```
If clean, done. If not, commit remaining changes.
