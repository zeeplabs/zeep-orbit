---
sidebar_position: 3
---

# Configuration

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `DATABASE_URL` | ✅ | PostgreSQL connection string |
| `DASHBOARD_BOOTSTRAP_SECRET` | ✅ | First-time admin setup secret |
| `GOOGLE_CLIENT_ID` | ❌ | Google OAuth Client ID (dashboard login) |
| `GOOGLE_CLIENT_SECRET` | ❌ | Google OAuth Client Secret |
| `GOOGLE_REDIRECT_URL` | ❌ | Google OAuth redirect URL |
| `GOOGLE_ALLOWED_DOMAINS` | ❌ | Comma-separated allowed email domains |
| `BRAND_THEME` | ❌ | Default theme (`azure`, `emerald`, `ruby`, `amber`, `orange`) |
| `BRAND_COMPANY_NAME` | ❌ | Company name for white-label |
| `LOG_LEVEL` | ❌ | Set `debug` for development output |

## App Names

App names support lowercase letters, numbers, hyphens (`-`), and underscores (`_`). Hyphens in the app name are automatically converted to underscores for the PostgreSQL schema (e.g. `my-app` → schema `my_app`, URL `/my-app/todos`).

## Apps and Tables

Apps, tables, columns, references, and indexes are created and edited entirely through the Dashboard (`/dashboard`) — there is no YAML file to author. Each app's JWT secret is generated automatically and stored in the database; each app's schema lives in its own PostgreSQL schema, isolated from every other app.

## Column Types

| Type | PostgreSQL |
|------|-----------|
| `text` | TEXT |
| `integer` | INTEGER |
| `bigint` | BIGINT |
| `decimal` | DECIMAL |
| `boolean` | BOOLEAN |
| `uuid` | UUID |
| `timestamptz` | TIMESTAMPTZ |
| `jsonb` | JSONB |

## Column Options

| Field | Description |
|-------|-------------|
| `required` | NOT NULL constraint |
| `unique` | UNIQUE constraint |
| `default` | DEFAULT value (SQL expression) |
| `rename_from` | Rename column on next provision |
| `references` | Foreign key to another table in the same app — see below |

## References (Foreign Keys)

A column can declare a foreign key to another table in the same app:

| Field | Description |
|-------|-------------|
| `table` | Target table name (must exist in the same app) |
| `column` | Target column — must be `id` or a column declared `unique` |
| `on_delete` | `cascade`, `restrict`, `set_null`, or `no_action` (default) |

Cross-app references are not supported — each app's schema is isolated. Circular foreign key dependencies between tables are rejected before any schema change is applied.

## Indexes

A table can declare any number of indexes, each with one or more columns:

| Field | Description |
|-------|-------------|
| `name` | Index name, unique across the app |
| `columns` | One or more column names, in the order the index should use |
| `unique` | Creates a `UNIQUE INDEX` instead of a regular one |

Indexes are created idempotently. Removing an index from a table's definition does **not** drop it from the database — dropping an index is a manual operation.
