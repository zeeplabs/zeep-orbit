# v0.1.9 — OpenAPI File Endpoints, Todo App Files, Docs Refresh

## Highlights

### 📜 Auto-Generated OpenAPI Docs for File Endpoints
Apps with storage config now get file management paths in their Swagger UI:

| Endpoint | Description |
|----------|-------------|
| `GET /{app}/files` | List files (paginated) |
| `POST /{app}/files` | Upload file (multipart) |
| `GET /{app}/files/{id}` | Get file metadata |
| `DELETE /{app}/files/{id}` | Delete file |
| `GET /{app}/files/{id}/download` | Redirect to signed S3 URL |
| `GET /{app}/files/{id}/url` | Get signed URL with TTL |

Each app with storage enabled now has a `file` schema in Components and all 6 endpoints documented with proper request/response schemas, auth requirements, and status codes.

### 📁 Todo Example App — File Management
The Todo example now includes a complete file management page:

- **File upload** with progress indicator
- **File list** with name, size, MIME type, and created date
- **Signed URL** generation for direct download
- **Delete** with confirmation
- Demonstrates `client.files.*` SDK usage in practice

### 📚 Docusaurus Docs Update
- **crud.md** — new sections for Sorting (`?order=field.desc`), Soft Delete (`?deleted=true`), and Per-App Health check endpoints
- **configuration.md** — new page documenting all config options
- **quickstart.md** — streamlined getting started guide

### ⬆️ CI — Node.js 24
Docker publish and docs workflows now use Node.js 24.

### 🐛 Bug Fixes
- Removed stale `.github/release-notes.md` that was tracking notes in the wrong location

### 📦 Docker
```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:v0.1.9
```

### 📋 Helm
```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit
helm install zeep-orbit zeeplabs/zeep-orbit
```
