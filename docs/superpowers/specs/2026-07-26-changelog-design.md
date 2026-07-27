# Changelog Feature — Design Spec

**Date:** 2026-07-26  
**Status:** Implemented (embed-based, revised)

## Purpose

In-app changelog page (similar to Easypanel) showing product updates per version. Data ships as a static JSON file embedded in the Go binary — no per-instance database storage. To add entries, edit the JSON file and commit it in a new release.

## Approach

**Key insight:** This is a self-hosted app — each user runs their own instance. If changelog entries were stored in the per-instance database, fresh installs would start empty. The correct approach for a self-hosted product is to ship the changelog data with the software itself.

- **`internal/dashboard/changelog.json`** — static JSON file with all release entries
- **`//go:embed changelog.json`** — embedded into the Go binary at compile time
- **`GET /dashboard/api/changelog`** — single endpoint serving the embedded JSON
- **No write endpoints, no DB table, no admin UI** — to update, edit the JSON file in the repo

## Data Format (changelog.json)

```json
{
  "entries": [
    {
      "version": "1.0.0",
      "release_date": "2026-07-26",
      "title": "Initial release",
      "summary": "",
      "sections": [
        {
          "type": "features",
          "items": [
            {"description": "Added feature X"}
          ]
        }
      ]
    }
  ]
}
```

Section types: `features`, `improvements`, `fixes`, `security`, `breaking`.

## API

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/changelog` | logado | Returns full embedded JSON (no pagination server-side) |

## Frontend

- Page at `/changelog`
- Timeline display with version badges, dates, colored section labels
- Client-side "Load more" pagination
- No admin controls (edit/delete/create) — data is source-controlled
- Sidebar link with Megaphone icon, positioned above user section
- Mobile bottom bar entry

## Files

### Backend
| File | Purpose |
|------|---------|
| `internal/dashboard/changelog.json` | Static changelog data |
| `internal/dashboard/changelog.go` | Embed + `ChangelogHandler` function |
| `internal/server/server.go` | Route: `GET /api/changelog` |

### Frontend
| File | Purpose |
|------|---------|
| `internal/dashboard/ui/src/pages/ChangelogPage.tsx` | Timeline page |
| `internal/dashboard/ui/src/App.tsx` | Route `/changelog` |
| `internal/dashboard/ui/src/pages/DashboardShell.tsx` | Sidebar + mobile links |
| `internal/dashboard/ui/src/locales/pt-BR.json` | Display-only i18n keys |
| `internal/dashboard/ui/src/locales/en.json` | Display-only i18n keys |

## How to Add a New Release Entry

1. Edit `internal/dashboard/changelog.json`
2. Add a new entry in the `entries` array (newest first)
3. Commit and create a new release
4. Users get the updated changelog on their next update
