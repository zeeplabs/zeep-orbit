---
sidebar_position: 5
---

# File Storage

Each app can optionally enable S3-compatible file storage (DigitalOcean Spaces, Magalu Cloud Storage, AWS S3, MinIO, etc.).

## Configuration

Enable storage in the Dashboard under **Storage (S3)** tab when creating/editing an app:

| Field | Description |
|-------|-------------|
| Bucket | S3 bucket name |
| Region | Region (e.g. `us-east-1`, `nyc3`) |
| Endpoint | Provider URL (e.g. `https://nyc3.digitaloceanspaces.com`) |
| Access Key ID | S3 access key |
| Secret Access Key | S3 secret key |

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/{app}/files` | Upload file (multipart) |
| GET | `/{app}/files` | List files |
| GET | `/{app}/files/{id}` | Get file metadata |
| GET | `/{app}/files/{id}/download?ttl=3600` | Download (302 → signed URL) |
| GET | `/{app}/files/{id}/url?ttl=3600` | Get signed URL |
| DELETE | `/{app}/files/{id}` | Delete file |

All endpoints require the app's JWT token.

## Example

```bash
# Upload
curl -X POST localhost:8080/myapp/files \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@photo.jpg"

# Response: {"id":"uuid","name":"photo.jpg","size":2048576,"mime_type":"image/jpeg","url":"/myapp/files/uuid/download"}

# Download (redirects to signed S3 URL)
curl -L localhost:8080/myapp/files/uuid/download \
  -H "Authorization: Bearer $TOKEN"

# Get signed URL with custom TTL
curl localhost:8080/myapp/files/uuid/url?ttl=7200 \
  -H "Authorization: Bearer $TOKEN"
# → {"url":"https://bucket.s3.amazonaws.com/...?AWSAccessKeyId=..."}
```
