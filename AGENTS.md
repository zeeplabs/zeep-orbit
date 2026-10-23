# AGENTS.md

Rules for any AI agent (Claude Code, Codex, Cursor, etc.) working in this repository. This file is the source of truth — `CLAUDE.md` only points here.

---

## 1. Project shape

- Go backend (`cmd/zeep`, `internal/*`) + React/Vite/TS dashboard (`internal/dashboard/ui`), embedded into the Go binary at build time.
- Schema-per-app PostgreSQL isolation. The **only** correct way to derive an app's Postgres schema name is `schemaNameForDB(appName)` (`strings.ReplaceAll(appName, "-", "_")`, no prefix). Never hardcode `"app_" + name` or any other pattern — this exact bug shipped to production once (see `CHANGELOG.md`, "Dashboard Google login" / app-users fixes).
- Apps and tables are managed **only** through the Dashboard (or future MCP server). There is no YAML config file to author or `apply` step — that flow was removed for good, don't reintroduce it.
- Larger features are spec'd under `.specs/features/<name>/` (`spec.md`, `design.md`, `tasks.md`) before implementation — follow this convention (`tlc-spec-driven`) for anything non-trivial. Small fixes/UI tweaks don't need a spec.

## 2. Branching and commits

- Day-to-day work happens on `develop`. `main` is release-only, and it's never pushed to directly — a release branch (`release-vX.Y.Z`, cut from `develop`) carries the version-bump commit, gets opened as a PR into `main`, and only lands after CI is green and it's reviewed. Merge via "Squash and merge" — `develop` carries a `merge --no-ff` reconciliation commit after every release, and GitHub's "Rebase and merge" can't replay merge commits via cherry-pick, so it fails once that commit is in the release branch's diff against `main` (recurring since v1.5.0). Full steps and rationale in `RELEASE.md`.
- Never commit directly to `main` outside that PR flow.
- Commit style follows `CONTRIBUTING.md`: `type: short description` (types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `release`). Keep messages about *why*, not just *what*.
- **Never commit unless explicitly asked to.** Staging/committing on your own initiative is not acceptable, even at the end of a task.
- Never `--force` push, `--amend` a pushed commit, or rewrite `main` history without explicit instruction.

## 3. Before considering any change done

Run and confirm clean:

- Backend: `go build ./...`, `go test -p 1 -parallel 4 ./...`, `go vet ./...`, `gofmt -l <changed files>`. The `-p 1 -parallel 4` matters: `internal/dashboard` and `internal/mcpserver` both run integration tests against the same `TEST_DATABASE_URL` and step on each other's `zeep_system` rows under Go's default cross-package parallelism (deadlocks / spurious FK violations, not a real bug) — this is what CI runs (`.github/workflows/reusable-ci.yml`), so match it locally.
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
4. Add an entry to `internal/dashboard/changelog.json` (renders on the dashboard's `/changelog` page). Every `title`/`summary`/`description` field is bilingual — `{"en": "...", "pt-BR": "..."}` — not an i18n key; the frontend (`ChangelogPage.tsx`) picks the field matching `i18n.language` at render time, falling back to `en`. Never add an entry with only one language.
5. Write `.github/release-notes-vX.Y.Z.md` — this becomes the actual GitHub Release body (`docker-publish.yml` reads it via `body_path`); if it's missing, CI silently falls back to GitHub's generic auto-generated notes.
6. Open a PR `release-vX.Y.Z` → `main`. Wait for CI green + review, then merge via "Squash and merge" (see section 2 and `RELEASE.md` for why not rebase).
7. Tag `vX.Y.Z` on `main`, push the tag — CI (`docker-publish.yml`) builds the image, publishes GHCR + GitHub Release + Helm chart package; `docs.yml` publishes the Helm chart repo from the `Chart.yaml` bump (triggered when the PR lands on `main`, not by the tag).
8. Publish SDK clients only if they actually changed (TS/Go/Python/Rust/Java/PHP — see `RELEASE.md` for per-client commands).

Never tag a release without CHANGELOG + both version files bumped — the dashboard's own update-available banner compares the live GitHub tag against `package.json`'s version, so a missed bump makes the banner nag users who are already current.

## 8. Risk and confirmation

- Treat schema migrations, Helm/K8s manifest changes, and anything touching `charts/`/`k8s/`/CI workflows as higher-risk — explain the change and, when in doubt, confirm before applying.
- Never disable/bypass a security check (auth middleware, CSRF state validation, rate limiting) to "make something work" without flagging it explicitly and getting confirmation first.
- Don't invent environment variables, secrets, or config surfaces that aren't documented in `README.md`'s configuration table or `RELEASE.md` — if you need a new one, add it to both.

## 9. Agent skills

Skills are versioned in the repo so every dev (and every agent) works from the same instructions. A skill is third-party code that runs with the agent's permissions — treat adding or updating one like adding a dependency.

**Layout**

- Canonical source: `.agents/skills/<name>/`. Claude Code reads it through a relative symlink `.claude/skills/<name>` → `../../.agents/skills/<name>`. Codex/Cursor read `.agents/skills/` directly.
- Exception: `orbit-internals` is internal-only and lives directly in `.claude/skills/`.
- `skills-lock.json` records source and upstream hash per skill. The hash is the **upstream** content, not the patched copy.

**Installed skills**

| Skill | Source | Use for |
|---|---|---|
| `orbit-usage` | `zeeplabs/zeep-orbit-agent-skills` | Building against an Orbit backend (REST, MCP, SDKs) |
| `orbit-internals` | this repo | Working on Orbit's own code (provisioning, handlers, tests) |
| `ui-ux-pro-max` | `nextlevelbuilder/ui-ux-pro-max-skill` | Dashboard UI/UX design, review and audit |
| `shadcn` | `shadcn-ui/ui` | shadcn components (`internal/dashboard/ui/components.json`) |
| `vercel-react-best-practices` | `vercel-labs/agent-skills` | React performance/patterns in the dashboard |
| `web-design-guidelines` | `vercel-labs/agent-skills` | UI/accessibility review checklist |
| `playwright-best-practices` | `currents-dev/playwright-best-practices-skill` | Dashboard e2e (`npm run test:e2e`) |
| `golang-security`, `golang-error-handling`, `golang-testing` | `samber/cc-skills-golang` | Go backend review/tests; complements section 4 |

These skills do **not** override this file. When a skill conflicts with a rule here (e.g. English-only copy conventions vs. the `en`/`pt-BR` i18n rule in section 5, or `-p 1` for Go tests), this file wins.

**Local patches (diverge from upstream — re-apply after any update)**

| Skill | Patch | Why |
|---|---|---|
| `shadcn` | Removed the `` !`npx shadcn@latest info --json` `` auto-run and the `allowed-tools` pre-approval; added a note to run from `internal/dashboard/ui` | Unpinned npm fetch on every skill load, and pre-approved `apply`/`init --force`/`add --all` overwrite component source without a prompt. The repo root has no `components.json`. |
| `web-design-guidelines` | Vendored upstream rules as `command.md` pinned to commit `434b7f9` (2026-10-05, sha256 in `SKILL.md`); `SKILL.md` reads the local file instead of using WebFetch on `main`; added Orbit i18n notes | Remote `main` content became agent instructions with no review and no reproducibility. |
| `golang-security` | `allowed-tools` reduced to `Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(govulncheck:*) AskUserQuestion` (dropped `Agent`, `WebFetch`, `WebSearch`, `Bash(git:*)`, worktree tools) | Pre-approval was broader than a review skill needs. Note: this only removes the auto-approval, it does not block the tool. |
| `golang-error-handling` | `allowed-tools` reduced to `Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) AskUserQuestion` (dropped `Bash(git:*)`, `Agent`) | Same as `golang-security`: `Bash(git:*)` pre-approved any git command, including push and reset. |
| `golang-testing` | `allowed-tools` reduced to `Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(gotests:*) AskUserQuestion` (dropped `Bash(git:*)`, `Agent`) | Same as above. |
| `ui-ux-pro-max` | Script commands use `.agents/skills/ui-ux-pro-max/scripts/search.py` (run from the repo root) instead of `${CLAUDE_PLUGIN_ROOT}/.claude/skills/...` | The variable is only set for plugin skills; in a project skill it is empty and the path resolved to `/.claude/skills/...`. |

Each patched `SKILL.md` carries a `Zeep Orbit local patch` note — keep it.

**Adding or updating a skill**

1. Pick a specific skill (not a whole repo). Prefer official sources and high install counts; check the source repo before installing.
2. `npx skills add <owner/repo> -s <skill> -a claude-code -y` (project scope, no `-g`). With CLI 1.7.x this copies into `.claude/skills/<name>/` even though its summary says `.agents/skills/`. Move it to `.agents/skills/<name>/` and replace it with the relative symlink described above.
3. Review before committing: `git diff skills-lock.json`, every `SKILL.md` frontmatter and body, and any `scripts/`. Reject or patch any skill that:
   - auto-runs commands on load (`` !`...` `` blocks),
   - fetches remote content at runtime and treats it as instructions,
   - pre-approves broad `allowed-tools` (`WebFetch`, `Bash(git:*)`, unpinned `npx pkg@latest *`), or
   - reads credentials, `.env`, or paths outside the repo.
4. Open a PR. Skill changes are reviewed like code. `npx skills update` rewrites files, including the patched ones above, so after every update re-check the table above and re-apply the patches.
5. Add or change a row in this section in the same PR.

**Known caveats**

- `npx skills experimental_install` (restore from the lock) has not been tested against the patched skills; it may overwrite the patches or choose a different location. Verify with `git diff` after use.
- The lock `skillPath` for `ui-ux-pro-max` is recorded as `.claude/skills/ui-ux-pro-max/SKILL.md`, unlike the other entries.
- Global skills with the same name (for example `vercel-react-best-practices`, `web-design-guidelines`) may exist on a dev's machine. Precedence between project and global skills is unverified.
- `ui-ux-pro-max/scripts/design_system.py` writes `design-system/<slug>/MASTER.md` in the working directory when persisting. Don't commit that output unless the change is intentional.
