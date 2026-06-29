---
sidebar_position: 6
---

# Rate Limiting

Each app can optionally enable per-IP rate limiting to protect your APIs from abuse.

## Configuration

Enable rate limiting in the Dashboard under the **API** tab when creating/editing an app:

| Field | Description |
|-------|-------------|
| Enabled | Activate rate limiting for this app |
| Requests per minute | Maximum requests per IP per 60-second window |

## How it works

- Sliding window algorithm — counts requests per unique IP address
- When the limit is exceeded, the API returns `429 Too Many Requests`
- Rate limit is reset after 60 seconds of inactivity from that IP
- Works on all `/{app}/*` routes (CRUD, auth, files)

## Example

```bash
# After exceeding the limit:
curl localhost:8080/myapp/items -H "Authorization: Bearer $TOKEN"
# → 429 {"error":"too many requests"}
```

The response includes a `Retry-After: 60` header indicating when the client can retry.
