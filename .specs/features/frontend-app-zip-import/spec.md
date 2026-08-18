# Frontend App ZIP Import Specification

## Problem Statement

Today a frontend app can only be created from a pre-registered GitHub template (`github_templates`), via `POST /dashboard/api/frontend-apps` → `CreateRepoFromTemplate` (GitHub "generate from template") → auto deploy on Render. Teams that already have working frontend code but no template registered cannot use this pipeline — they have no path to get a repo + deploy without first turning their code into a template. This feature adds a second creation path: upload a ZIP of existing code, and get the same end state (private GitHub repo, code committed, Render static site deployed) without needing a template.

## Goals

- [ ] A user can create a frontend app by uploading a ZIP instead of picking a template, ending in the same three artifacts template creation produces: GitHub repo, committed code, deployed Render static site.
- [ ] Malformed, oversized, or unsafe ZIP content (path traversal, zip bombs) is rejected before any GitHub/Render resource is created.
- [ ] The existing template creation path (request/response contract, synchronous behavior) is unchanged.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature                                                                 | Reason                                                                                                                                                        |
| ------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Retrying a failed ZIP import by reusing the original upload             | The raw ZIP bytes are not persisted after processing (avoids storing arbitrary uploaded code at rest). A failed import must be deleted and re-uploaded fresh. |
| Custom build/publish/start command input for ZIP-sourced apps           | Decision: fixed static-site preset only for this feature (see Assumptions). Configurable deploy commands for ZIP imports is a future feature.               |
| Replacing/updating the code of an existing app via a new ZIP upload      | This feature covers creation only, not redeploy-by-reupload.                                                                                                 |
| Registering a ZIP-uploaded project as a reusable template               | Turning uploaded code into a `github_templates` entry is a separate, unrelated feature.                                                                      |
| Antivirus / malware scanning of uploaded ZIP content                    | No scanning infrastructure exists in this project today; adding one is out of scope for this feature.                                                        |
| `backend_app_id` / `subdomain` fields in the new ZIP tab                | The existing template tab does not expose these fields either (current UI gap); this feature keeps parity, not closes that gap.                             |
| Multi-file (non-ZIP) upload                                             | Only a single `.zip` archive is accepted.                                                                                                                    |

---

## Assumptions & Open Questions

Every ambiguity is resolved or recorded here - nothing is left silently unclear.

| Assumption / decision                                                                                       | Chosen default                                                                                                                    | Rationale                                                                                                                                                             | Confirmed? |
| -------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- |
| Deploy config source for ZIP-sourced apps                                                                      | Fixed static-site preset, no user input                                                                                             | User decision.                                                                                                                                                        | y          |
| Max ZIP upload size                                                                                            | 100 MB (`http.MaxBytesReader` on the request body, same ceiling as the existing `storage_handler.go` upload endpoint)               | User decision (reuse existing precedent in codebase).                                                                                                                | y          |
| Creation flow UI placement                                                                                     | Tabs/toggle inside the existing single `Dialog` ("From template" / "Import ZIP")                                                    | User decision.                                                                                                                                                        | y          |
| Local↔repo sync behavior for ZIP-sourced apps                                                                  | Identical to template-sourced apps (`setupSync` runs automatically once the repo is ready)                                          | User decision — sync is generic repo infrastructure, does not depend on how the repo was populated.                                                                  | y          |
| Creation synchronicity                                                                                         | Asynchronous: the request returns immediately with `status: "processing"`; a background goroutine does extract → validate → create repo → commit → sync → deploy | User decision, given ZIP processing (unzip, per-file blob upload via GitHub Git Data API) can run materially longer than the template path's single atomic GitHub call. | y          |
| Exact static-site preset values                                                                                | `render_service_type = "static_site"`, `build_command = "npm install && npm run build"`, `publish_path = "dist"`, `start_command = ""` | Matches the org's dominant frontend stack (React/Vite) and the existing `static_site` handling already implemented in `render.go`. Shown to the user before upload so they can self-check their project matches. | n (agent default — confirm before implementing if the target stack is commonly non-npm/non-`dist`) |
| Zip-bomb protection thresholds                                                                                 | Reject if uncompressed total > 500 MB (5× the compressed limit) or entry count > 5000                                              | Standard zip-bomb heuristic (compression-ratio + entry-count ceiling); no existing precedent in this codebase to reuse.                                              | n (agent default) |
| Path-safety validation                                                                                         | Reject the whole archive if any entry has an absolute path, a `..` path segment, or is a symlink                                    | Standard zip-slip mitigation; a security invariant, not a product choice.                                                                                            | y (security invariant, non-negotiable) |
| Single top-level wrapper folder                                                                                | If every entry in the ZIP shares one common top-level directory (e.g. `myapp/`), that prefix is stripped before committing          | Common UX pattern when a user zips a project folder directly (mirrors how tarballs/most zip tools behave); without this, a valid project would be rejected for "no `package.json` at root". | n (agent default) |
| Minimum content validation                                                                                     | Reject if no `package.json` is found at the (normalized) archive root                                                              | Cheapest possible signal that "this looks like a frontend project", checked before any GitHub resource is created (fail fast).                                      | n (agent default) |
| Partial-failure cleanup                                                                                        | No automatic rollback: a repo created before a later step (commit/deploy) fails is left in place, same as the existing template path | Consistent with the already-shipped `frontend-app-entity` / `deploy-provider-integration` decisions ("failure never loses the record", "failure in any stage does not undo repo/deploy key already created"). | y (matches existing shipped behavior) |
| New `frontend_apps.status` value                                                                                | Add `processing` alongside the existing `ready` / `failed`, used only on the ZIP path — the template path never produces `processing` | Preserves the template path's existing synchronous contract exactly as required by Goals; only the new path introduces the new state.                               | y |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Import an app by uploading a ZIP ⭐ MVP

**User Story**: As a dashboard user with existing frontend code, I want to upload a ZIP of that code so a private GitHub repo is created, my code is committed to it, and a Render static site is deployed — without needing a pre-registered template.

**Why P1**: This is the entire point of the feature; without it there is nothing to ship.

**Acceptance Criteria**:

1. WHEN a user submits the "Import ZIP" tab with a name and a `.zip` file THEN the system SHALL respond immediately with the created `frontend_apps` record at `status: "processing"`, without waiting for repo creation, commit, or deploy to finish.
2. The system SHALL display, on the "Import ZIP" tab, the fixed deploy preset (build command `npm install && npm run build`, publish directory `dist`, static site) before the user selects a file, so the user can verify their project matches.
3. WHEN the background import completes repo creation, code commit, and (if configured) Render deploy successfully THEN the system SHALL update the record to `status: "ready"` with `github_repo_url` populated, mirroring the fields already used by the template path.
4. WHEN a ZIP contains a single common top-level directory THEN the system SHALL strip that prefix before committing, treating the directory's contents as the archive root.
5. IF the (normalized) archive root has no `package.json` THEN the system SHALL reject the import with `status: "failed"` and an error message stating a `package.json` at the project root is required, without creating any GitHub repo.
6. IF the uploaded file is not a valid ZIP archive THEN the system SHALL reject the request with a 400 response before any processing starts.
7. IF the uploaded file exceeds 100 MB THEN the system SHALL reject the request with a 413/400 response before any processing starts.
8. IF the ZIP's total uncompressed size exceeds 500 MB or it contains more than 5000 entries THEN the system SHALL reject the import as `status: "failed"` with a size-limit error message, without creating any GitHub repo.
9. IF any entry in the ZIP has an absolute path, a `..` path segment, or is a symlink THEN the system SHALL reject the entire import as `status: "failed"` with a security-validation error message, without creating any GitHub repo.
10. IF the chosen app name's slug already exists among non-archived frontend apps THEN the system SHALL reject the request with the same 409 behavior the template path already has.
11. IF no GitHub App is connected for the instance THEN the system SHALL reject the request with the same error behavior the template path already has.
12. IF repo creation succeeds but committing the extracted code fails (e.g. GitHub API error mid-commit) THEN the system SHALL set `status: "failed"` with the underlying error message, leaving the created repo in place for manual inspection or deletion.
13. WHEN the repo and commit succeed but the configured Render deploy call fails THEN the system SHALL set the app's overall `status: "ready"` while setting `deploy_status: "failed"` with `deploy_error_message` populated — identical to how the template path already separates creation success from deploy success.
14. The system SHALL run local↔repo sync setup (`setupSync`) for a successfully created ZIP-sourced app the same way it already does for a template-sourced app.

**Independent Test**: Upload a small valid ZIP containing a `package.json` and a couple of source files through the "Import ZIP" tab, observe the record go from `processing` to `ready` with a live `github_repo_url`, and confirm the repo on GitHub contains the uploaded files and Render shows a deployed static site.

---

### P2: Manage ZIP-sourced apps the same way as template-sourced apps

**User Story**: As a dashboard user, I want listing, deleting, and syncing to behave the same regardless of whether an app was created from a template or a ZIP, so I don't need to learn two different management flows.

**Why P2**: Without this, the new creation path would produce records that behave inconsistently once created — a real gap, but the app is demoable and useful without polishing every management action on day one.

**Acceptance Criteria**:

1. WHEN listing frontend apps THEN the system SHALL include ZIP-sourced apps in the same list, in the same shape, as template-sourced apps.
2. WHEN a user deletes a ZIP-sourced app (in any status, including `processing` or `failed`) THEN the system SHALL perform the same soft-delete, best-effort repo archive, deploy-key revoke, and Render service delete sequence already used for template-sourced apps.
3. WHILE a ZIP-sourced app is `status: "processing"` the system SHALL prevent a second concurrent ZIP import from reusing the same name/slug, consistent with the existing slug-uniqueness constraint.

**Independent Test**: Create one ZIP-sourced and one template-sourced app, confirm both appear in the same list, then delete the ZIP-sourced one and confirm repo/deploy-key/Render cleanup happens exactly as it does for a deleted template-sourced app.

---

### P3: See processing status update without a manual page reload

**User Story**: As a dashboard user, I want the UI to automatically reflect a ZIP import moving from "processing" to "ready"/"failed" so I don't have to keep refreshing the page.

**Why P3**: Nice-to-have UX polish; without it the user can still get the same information via a manual refresh, so it does not block MVP usefulness.

**Acceptance Criteria**:

1. WHILE a visible frontend app row has `status: "processing"` the dashboard SHALL poll for its updated status on a fixed interval until it leaves `processing`.

---

## Edge Cases

- IF the ZIP contains zero files THEN the system SHALL reject it as invalid (same path as "no `package.json` found").
- IF the ZIP's `package.json` is present but the archive contains no other files (empty project) THEN the system SHALL still proceed — content validity beyond the `package.json` presence check is the user's responsibility, matching the "trust the uploader, validate structure not semantics" scope of this feature.
- WHEN the same user submits two ZIP imports with different names back-to-back THEN both SHALL be processed independently and concurrently (no artificial serialization beyond the existing per-slug uniqueness constraint).
- IF the request is aborted/disconnected by the client after the initial "processing" response THEN the system SHALL continue the background import to completion regardless (client disconnect does not cancel server-side work already accepted).

---

## Requirement Traceability

Each requirement gets a unique ID for tracking across design, tasks, and validation.

| Requirement ID | Story                    | Phase  | Status  |
| -------------- | ------------------------ | ------ | ------- |
| ZIP-01         | P1: Import via ZIP       | Design | Pending |
| ZIP-02         | P1: Import via ZIP       | Design | Pending |
| ZIP-03         | P1: Import via ZIP       | Design | Pending |
| ZIP-04         | P1: Import via ZIP       | Design | Pending |
| ZIP-05         | P1: Import via ZIP       | Design | Pending |
| ZIP-06         | P1: Import via ZIP       | Design | Pending |
| ZIP-07         | P1: Import via ZIP       | Design | Pending |
| ZIP-08         | P1: Import via ZIP       | Design | Pending |
| ZIP-09         | P1: Import via ZIP       | Design | Pending |
| ZIP-10         | P1: Import via ZIP       | Design | Pending |
| ZIP-11         | P1: Import via ZIP       | Design | Pending |
| ZIP-12         | P1: Import via ZIP       | Design | Pending |
| ZIP-13         | P1: Import via ZIP       | Design | Pending |
| ZIP-14         | P1: Import via ZIP       | Design | Pending |
| ZIP-15         | P2: Manage consistently  | Design | Pending |
| ZIP-16         | P2: Manage consistently  | Design | Pending |
| ZIP-17         | P2: Manage consistently  | Design | Pending |
| ZIP-18         | P3: Live status polling  | Design | Pending |

**ID format:** `ZIP-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 18 total, 0 mapped to tasks, 18 unmapped ⚠️ (expected at Specify stage — mapping happens in Design/Tasks)

---

## Success Criteria

How we know the feature is successful:

- [ ] A user can go from "I have a ZIP of frontend code" to "a live Render static site backed by a private GitHub repo" without ever registering a template.
- [ ] No unsafe ZIP (path traversal, zip bomb, oversized) ever results in a GitHub API call being made.
- [ ] Zero regressions in the existing template creation path's request/response contract or synchronous behavior.
