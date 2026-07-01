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

App names support lowercase letters, numbers, hyphens (`-`), and underscores (`_`). Hyphens in the app name are automatically converted to underscores for the PostgreSQL schema.

```yaml
apps:
  - name: my-app  # URL: /my-app/todos, Schema: my_app
```

## apps.yaml

```yaml
platform:
  database_url: ${DATABASE_URL}

apps:
  - name: myapp
    auth:
      jwt_secret: ${MYAPP_JWT_SECRET}
      providers:
        email: true
    tables:
      - name: items
        columns:
          - { name: title, type: text, required: true }
          - { name: score, type: decimal }
```

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
