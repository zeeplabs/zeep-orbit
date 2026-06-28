# Roadmap

## M1 — MVP Core ✅ (concluído)

Schema YAML → REST APIs on PostgreSQL. CLI-driven. Docker Compose deploy.

**Features entregues:** Config loader, schema provisioning, CRUD REST API, JWT auth, CLI (`serve`/`apply`/`list`/`status`), Docker Compose, query params.

---

## M2 — Developer Experience ✅ (concluído)

- [x] Web dashboard (app catalog, data browser, logs, user management)
- [x] Helm chart + Kubernetes deploy
- [x] Filtering (`gt.`, `gte.`, `lt.`, `lte.`, `like.`, `ilike.`, `in.`, `ne.`)
- [x] Sorting (`?order=field.asc`)
- [x] Native email/password auth per app
- [x] Google OAuth per app
- [x] OpenAPI/Swagger docs per app
- [x] Row-Level Security (`rls: owner`)

### Próximos (M2 pendente)

- [ ] TypeScript SDK (`@zeep/client`)
- [ ] Official prompt snippets for Claude Code / Cursor / Lovable
- [ ] MCP server for zeep-orbit operations
- [ ] Schema migrations (alter existing tables safely)

---

## M3 — Governance & Security

- [ ] Audit log (who did what, when, on which app)
- [ ] Role-based access per app (admin, editor, viewer)
- [ ] Corporate SSO integration (Google Workspace, Microsoft Entra ID)
- [ ] Rate limiting per app
- [ ] Schema change approval workflow
- [ ] Multi-factor authentication

---

## M4 — Storage & Events

- [ ] File storage per app (S3/MinIO compatible)
- [ ] Signed upload/download URLs
- [ ] Webhook support (on insert/update/delete)
- [ ] Event bus integration

---

## Deferred

- GraphQL auto-generation
- Realtime subscriptions
- Edge functions
- Multi-region support
- Marketplace of app templates
