# AGENTS.md

Rules for any AI agent (Claude Code, Codex, Cursor, etc.) working in this repository. This file is the source of truth — `CLAUDE.md` only points here.

---

## 1. Project shape

- Go backend (`cmd/zeep`, `internal/*`) + React/Vite/TS dashboard (`internal/dashboard/ui`), embedded into the Go binary at build time.
- Schema-per-app PostgreSQL isolation. The **only** correct way to derive an app's Postgres schema name is `schemaNameForDB(appName)` (`strings.ReplaceAll(appName, "-", "_")`, no prefix). Never hardcode `"app_" + name` or any other pattern — this exact bug shipped to production once (see `CHANGELOG.md`, "Dashboard Google login" / app-users fixes).
- Apps and tables are managed **only** through the Dashboard (or future MCP server). There is no YAML config file to author or `apply` step — that flow was removed for good, don't reintroduce it.
- Larger features are spec'd under `.specs/features/<name>/` (`spec.md`, `design.md`, `tasks.md`) before implementation — follow this convention (`tlc-spec-driven`) for anything non-trivial. Small fixes/UI tweaks don't need a spec.

## 2. Branching and commits

- Day-to-day work happens on `develop`. `main` is release-only, and it's never pushed to directly — a release branch (`release-vX.Y.Z`, cut from `develop`) carries the version-bump commit, gets opened as a PR into `main`, and only lands after CI is green and it's reviewed. Merge via "Rebase and merge" (not "Create a merge commit") to keep `main`'s history linear. Full steps in `RELEASE.md`.
- Never commit directly to `main` outside that PR flow.
- Commit style follows `CONTRIBUTING.md`: `type: short description` (types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `release`). Keep messages about *why*, not just *what*.
- **Never commit unless explicitly asked to.** Staging/committing on your own initiative is not acceptable, even at the end of a task.
- Never `--force` push, `--amend` a pushed commit, or rewrite `main` history without explicit instruction.

## 3. Before considering any change done

Run and confirm clean:

- Backend: `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l <changed files>`
- Frontend (`internal/dashboard/ui`): `npx tsc -b`, `npm run build`
- If you touched i18n JSON: validate with `python3 -c "import json; json.load(open('src/locales/en.json'))"` (and `pt-BR.json`)

Don't report a task as complete without having actually run these.

## 4. Backend rules

- **API error strings are always in English.** Localization/display is the frontend's job (`toast.error(error.message)` today) — never translate error messages on the server. This was an explicit, repeated correction — treat it as a hard rule.
- Never store CSRF/session/OAuth state in an in-memory map keyed by process. This service runs multiple replicas behind a non-sticky load balancer — anything in-memory that a later request depends on will randomly fail depending on which pod handles it. Use signed, stateless tokens instead (HMAC-SHA256 with an `exp` claim is the established pattern — see `internal/auth/google.go` `signState`/`verifyState`, and `internal/dashboard/google.go` `signGoogleState`/`verifyGoogleState`).
- User-facing text fields written by users (name, email) get normalized before persisting: email lowercased, name Title-Cased, format-validated with `net/mail.ParseAddress` (stdlib, no extra deps). Apply this pattern at every new registration/profile-write endpoint, not just the ones that already have it.
- When a config/update endpoint supports partial updates (merge-on-absent-key semantics), any frontend form that lets a user **clear** a field must always send that field's key explicitly (empty value/array), never omit it. Omission is read as "don't touch this field," so clearing silently no-ops. Check `mergeProviderConfig` in `internal/dashboard/auth_providers_store.go` for the existing pattern before adding a new mergeable config surface.
- Don't leak raw internal errors (`err.Error()`) into HTTP responses for 500s. Log the real error server-side, return a fixed generic message to the client. Typed errors (like `provisioner.TypeChangeError`) that are safe to expose are the exception, not the default.

## 5. Frontend rules

- Every user-facing string goes through `react-i18next` (`t()`/`Trans`), added to **both** `src/locales/en.json` and `src/locales/pt-BR.json` in the same change. No hardcoded Portuguese or English strings in components — this has been a recurring audit finding.
- Any endpoint whose result is needed before the first meaningful paint (theme, feature flags visible on first render) should be fetched through a shared hook (see `usePublicConfig()` pattern in `src/lib/api.ts`) and gated behind the app's existing `LoadingScreen`, not fetched ad-hoc per-page with `useEffect`/ungated `useQuery`. Redundant fetches of the same endpoint from multiple components are a bug, not a shortcut.
- Toast on mutation errors (`sonner`, `toast.error(error.message)`) — a mutation hook without `onError` is an incomplete hook.

## 6. Documentation that must stay in sync

- `CHANGELOG.md` — add an entry under `## [Unreleased]` in the **same change** that ships the fix/feature. Don't defer this to release day.
- `README.md` feature tables — update when a feature is genuinely new/user-facing. Internal bug fixes don't need a README line; CHANGELOG is enough for those.
- **Translated READMEs** — this repo ships 4: `README.md` (English, canonical), `i18n/README.pt-BR.md`, `i18n/README.pt-PT.md`, `i18n/README.es.md`. If you add/change a row in README.md's feature tables, mirror the change in all 3 translations in the same change — don't let them drift. Each translation keeps its own relative paths (`../docs/...`, `../LICENSE`, etc.) since they live one directory deeper than the root.
- `RELEASE.md` — the authoritative release process. If you discover the real release process has more/different steps than what's documented (as happened once — `package.json` version bump and the in-app `changelog.json` entry were both missing from the doc), fix the doc, don't just do the extra step silently.

## 7. Releases

Follow `RELEASE.md` exactly, in order. Do not skip steps or reorder them. In short:

1. Cut `release-vX.Y.Z` from `develop`.
2. Bump version in **both** `charts/zeep-orbit/Chart.yaml` (`version` + `appVersion`) and `internal/dashboard/ui/package.json`.
3. Move `[Unreleased]` entries in `CHANGELOG.md` into a new dated version section.
4. Add an entry to `internal/dashboard/changelog.json` (renders on the dashboard's `/changelog` page).
5. Write `.github/release-notes-vX.Y.Z.md` — this becomes the actual GitHub Release body (`docker-publish.yml` reads it via `body_path`); if it's missing, CI silently falls back to GitHub's generic auto-generated notes.
6. Open a PR `release-vX.Y.Z` → `main`. Wait for CI green + review, then merge via "Rebase and merge."
7. Tag `vX.Y.Z` on `main`, push the tag — CI (`docker-publish.yml`) builds the image, publishes GHCR + GitHub Release + Helm chart package; `docs.yml` publishes the Helm chart repo from the `Chart.yaml` bump (triggered when the PR lands on `main`, not by the tag).
8. Publish SDK clients only if they actually changed (TS/Go/Python/Rust/Java/PHP — see `RELEASE.md` for per-client commands).

Never tag a release without CHANGELOG + both version files bumped — the dashboard's own update-available banner compares the live GitHub tag against `package.json`'s version, so a missed bump makes the banner nag users who are already current.

## 8. Risk and confirmation

- Treat schema migrations, Helm/K8s manifest changes, and anything touching `charts/`/`k8s/`/CI workflows as higher-risk — explain the change and, when in doubt, confirm before applying.
- Never disable/bypass a security check (auth middleware, CSRF state validation, rate limiting) to "make something work" without flagging it explicitly and getting confirmation first.
- Don't invent environment variables, secrets, or config surfaces that aren't documented in `README.md`'s configuration table or `RELEASE.md` — if you need a new one, add it to both.
