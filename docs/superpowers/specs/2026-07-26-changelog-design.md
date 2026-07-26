# Changelog Feature — Design Spec

**Date:** 2026-07-26
**Status:** Design approved

## Purpose

In-app changelog page (similar to Easypanel) that lets users see product updates per version. Superadmin can manage entries via the dashboard. Retroactive insertion supported — past releases can be added later.

## Data Model

Table `zeep_system.changelog_entries` (idempotent DDL in provisioner.go):

```sql
CREATE TABLE IF NOT EXISTS zeep_system.changelog_entries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version      TEXT NOT NULL,
    release_date DATE NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    sections     JSONB NOT NULL DEFAULT '[]',
    published    BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- `version` — e.g. `"1.0.0"` or `"2026.07.15"`
- `sections` — JSONB array of `{type: string, items: [{description: string}]}`
- `published` — allows draft entries (superadmin can toggle)
- `title` / `summary` — optional header text for the release

## API Endpoints

All under `/dashboard/api/changelog`. Read requires auth (any logged-in user). Write requires superadmin.

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/changelog` | logado | List entries (query: `?limit=10&offset=0`), ordered by `release_date DESC` |
| `POST` | `/api/changelog` | superadmin | Create entry |
| `PUT` | `/api/changelog/{id}` | superadmin | Update entry |
| `DELETE` | `/api/changelog/{id}` | superadmin | Delete entry |

GET filters `WHERE published = true` for all users (including superadmin). Unpublished entries are not shown on the timeline — they only appear in the superadmin edit list.

GET response:
```json
{
  "entries": [
    {
      "id": "uuid",
      "version": "1.2.0",
      "release_date": "2026-07-20",
      "title": "Changelog feature",
      "summary": "",
      "sections": [
        {
          "type": "features",
          "items": [{ "description": "Added changelog page" }]
        }
      ],
      "published": true,
      "created_at": "..."
    }
  ],
  "total": 25
}
```

POST/PUT body matches the entry shape (minus id, created_at, updated_at).

## Files to Create / Modify

### Backend (Go)

| File | Action | Purpose |
|------|--------|---------|
| `internal/dashboard/provisioner.go` | Modify | Add `changelog_entries` DDL |
| `internal/dashboard/changelog_store.go` | Create | CRUD queries (List, Create, Update, Delete) |
| `internal/dashboard/changelog.go` | Create | HTTP handlers |
| `internal/server/server.go` | Modify | Register 4 changelog routes under `/dashboard` |

### Frontend (React/TypeScript)

| File | Action | Purpose |
|------|--------|---------|
| `internal/dashboard/ui/src/pages/ChangelogPage.tsx` | Create | Timeline page with load-more, admin controls |
| `internal/dashboard/ui/src/App.tsx` | Modify | Add `/changelog` route |
| `internal/dashboard/ui/src/pages/DashboardShell.tsx` | Modify | Add changelog nav item + sidebar link |

### i18n

| File | Action | Purpose |
|------|--------|---------|
| `internal/dashboard/ui/src/locales/pt-BR.json` | Modify | Add changelog strings |
| `internal/dashboard/ui/src/locales/en.json` | Modify | Add changelog strings |

## Frontend Design

### Page layout (`/changelog`)

- Full-width timeline, centered column (max-w-2xl)
- Each entry: version badge, date, optional title/summary, then sections
- Sections grouped by type with colored labels:
  - `features` → green/emerald, label "Novidades"
  - `improvements` → blue, label "Melhorias"
  - `fixes` → amber, label "Correções"
  - `security` → red, label "Segurança"
  - `breaking` → purple, label "Breaking Changes"
- "Carregar mais" button at bottom (increments offset)
- If superadmin: edit (pencil) and delete (trash) icon buttons inline on each entry
- If superadmin: "Adicionar entrada" button at top that opens a create/edit modal
- Modal has fields: version, release_date (date picker), title, summary, sections (add/remove section + add/remove items per section)

### Sidebar placement

In `navItems()`, add changelog as a fixed bottom item in the nav list (before the user section). In the sidebar JSX, render it in the gap between the `<nav>` flex area and the user `<div>`:

```tsx
{/* Changelog — positioned above user section */}
<NavLink to="/changelog" ...>
  <Megaphone size={15} strokeWidth={1.5} />
  Changelog
</NavLink>
{/* User section divider */}
<div style={{ borderTop: "1px solid rgba(255,255,255,0.06)", paddingTop: 14 }}>...</div>
```

Also add a `Megaphone` icon entry in the mobile `BottomBar`.

### Configuration

No configuration needed beyond the DB table — the feature is always available.

## Retroactive Insertion

All entries live in the database, so past releases can be inserted:

1. Via the dashboard modal (one entry at a time)
2. Via SQL `INSERT INTO zeep_system.changelog_entries (...) VALUES (...)` — for bulk or retroactive data

Both approaches work identically. No need for a CSV/JSON import endpoint.

## Testing

- Unit tests for store CRUD (pattern: `changelog_store_test.go`)
- Manual E2E: create entry via modal → visible on timeline → edit → delete

## Out of Scope

- Public (unauthenticated) changelog
- RSS feed
- Notification badges (e.g. "new since last visit")
- Draft/preview mode beyond the `published` boolean (superadmin can toggle it per entry, but there is no separate "preview" view)
