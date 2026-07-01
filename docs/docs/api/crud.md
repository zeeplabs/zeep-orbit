---
sidebar_position: 1
---

# CRUD Endpoints

All app routes require a Bearer JWT signed with the app's `jwt_secret` (HS256).

| Method | Path | Description |
|--------|------|-------------|
| GET | `/{app}/{table}` | List records |
| POST | `/{app}/{table}` | Create record |
| GET | `/{app}/{table}/{id}` | Get by ID |
| PUT/PATCH | `/{app}/{table}/{id}` | Update (partial) |
| DELETE | `/{app}/{table}/{id}` | Delete (or soft-delete if enabled) |
| GET | `/{app}/health` | App health check (no auth required) |
| POST | `/{app}/files` | Upload file (multipart) |
| GET | `/{app}/files` | List files |
| GET | `/{app}/files/{id}` | Get file metadata |
| GET | `/{app}/files/{id}/download` | Download file (302 → signed URL) |
| GET | `/{app}/files/{id}/url` | Get signed URL |
| DELETE | `/{app}/files/{id}` | Delete file |

## Example

```bash
TOKEN=$(jwt encode --secret "$MY_JWT_SECRET" '{}')

# Create
curl -X POST localhost:8080/billing/invoices \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"amount": 150.00, "status": "pending"}'

# List
curl localhost:8080/billing/invoices \
  -H "Authorization: Bearer $TOKEN"

# Get
curl localhost:8080/billing/invoices/018e4c72-... \
  -H "Authorization: Bearer $TOKEN"
```

## Sorting

Use `?order=field.desc` or `?order=field.asc`:

```bash
curl "localhost:8080/billing/invoices?order=created_at.desc"
```

## Soft Delete

When soft delete is enabled (Dashboard → Settings → Soft Delete), the `DELETE` endpoint performs an `UPDATE SET deleted_at = now()` instead. Listed records automatically exclude soft-deleted rows. Use `?deleted=true` to include them:

```bash
curl "localhost:8080/billing/invoices?deleted=true"
```

## Health Check

Each app exposes an unauthenticated health endpoint:

```bash
curl localhost:8080/my-app/health
# {"status":"ok","app":"my-app","healthy":true,"checks":{"database":true,"schema":true}}
```

## Response Format

**List:**
```json
{ "data": [...], "count": 42, "limit": 50, "offset": 0 }
```

**Single record:**
```json
{ "id": "018e4c72-...", "title": "Hello", "created_at": "...", "updated_at": "..." }
```

**Error:**
```json
{ "error": "not found" }
```
