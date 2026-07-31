## v0.5.1

### ✨ Added

- **Render Environment ID field in Deploy Provider config** (superadmin, GitHub Integration → Deploy tab) — needed because Render assigns new services to an Environment, not a Project. The Project ID alone only works when that Project has exactly one Environment (auto-resolved); Projects with multiple Environments now require this field explicitly.

### 🐛 Fixed

- **GitHub Integration page had no role guard on the frontend** — every action there (Config, Templates, Deploy tabs) is `superadmin`-only server-side, but an `admin` navigating to the page directly saw the forms render and silently 403 on every request. The page now checks the current user's role and shows a clear "superadmin access required" message instead.
- **Admin users couldn't see GitHub templates in the frontend-app creation modal** — the templates list endpoint was restricted to `superadmin`, but the modal (used by any authenticated role) calls that same endpoint and silently swallowed the resulting 403, leaving the template select empty. Listing is now open to any authenticated role; managing templates stays `superadmin`-only.
- **Frontend app deploy could leave an orphaned Render service after a failure**, permanently blocking that name — the Render service ID is now persisted immediately after Render confirms creation, before any further step (custom domain, etc.) that could fail.
- **Frontend app creation intermittently failed to deploy** with transient `404`/`500` errors from Render right after creating the service — `CreateService` now retries those two statuses with backoff (up to 4 attempts); other errors (name conflict, rate limit) still fail immediately.
- **Deployed Render services were never actually placed in the configured Project** — Render's create-service API has no `projectId` field; services are assigned via `environmentId`. The configured Project ID is now resolved to its Environment and sent correctly on creation.

### Upgrade notes

No breaking changes. A new `render_environment_id` column is added to `deploy_provider_config` via an idempotent migration (`ADD COLUMN IF NOT EXISTS`) — no manual migration steps. If your configured Render Project has more than one Environment, set the new Environment ID field in Integrations → GitHub → Deploy, or deploys will fail with a clear error instead of guessing.
