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
| DELETE | `/{app}/{table}/{id}` | Delete |

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
