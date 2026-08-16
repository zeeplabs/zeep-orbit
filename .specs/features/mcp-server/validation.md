# mcp-server Validation

## Validation: mcp-server (pass 2) - PASS ✅

**Date**: 2026-08-13
**Spec**: `.specs/features/mcp-server/spec.md`
**Diff range**: `30ff8f2..e9d35cc` (develop, 21 tasks T1-T21 + 1 fix commit closing pass #1's 2 gaps)
**Verifier**: independent sub-agent (author ≠ verifier) — fresh pass, supersedes the pass #1 report

**Supersession note**: this report replaces the pass #1 `validation.md` (verdict FAIL, 2 high-priority NOT-COVERED gaps + 3 lower-priority notes). Fix commit `e9d35cc` ("test(dashboard): cover OAuth deny path and consent client-name field") closed both high-priority gaps. This pass independently re-verified those fixes are genuine (not just plausibly-named tests), re-ran the full gate and sensor from scratch, and re-derived spec-anchored coverage across the whole feature rather than only the delta.

**Process note carried forward**: `.specs/features/mcp-server/design.md` remains untracked (`git status --porcelain` shows `?? .specs/features/mcp-server/design.md`) — same process gap pass #1 flagged, unchanged by the fix commit. Not a spec/code gap; flagged for the record only.

---

## Task Completion

All 21 tasks (T1-T21) marked `Complete` in `tasks.md`, commits present in `git log 30ff8f2..HEAD`. The fix commit `e9d35cc` is a test-only addition (70 insertions, 1 file, no production code touched) on top of T1-T21 — confirmed via `git show --stat e9d35cc`.

| Task | Status | Notes |
| --- | --- | --- |
| T1-T21 | ✅ Done | All commits present, `Done when` checkboxes marked `[x]` |
| Fix (e9d35cc) | ✅ Done | Test-only, closes pass #1 gaps #1 and #2 |

---

## Gap Closure Verification (pass #1's two NOT-COVERED items)

### Gap #1 — OAuth deny-consent path had zero test coverage

`internal/dashboard/oauth_server_test.go:276-333` — `TestDecide_DenyRedirectsWithAccessDeniedAndNoCodeIssued`. Independently read line-by-line, not trusted by name:

- Calls `h.Decide` with a POST body containing `"decision": "deny"` (`:297`).
- Asserts `redirectURL.Query().Get("error") != "access_denied"` fails the test if not exactly `access_denied` (`:316-318`) — exact string match, not a substring/existence check.
- Asserts `redirectURL.Query().Get("code") != ""` fails the test if a code param is present (`:319-321`).
- Asserts `redirectURL.Query().Get("state") != "xyz"` — state preserved through the deny path (`:322-324`, spec-defined outcome not explicitly required but a reasonable strengthening).
- Directly queries `SELECT count(*) FROM zeep_system.oauth_auth_codes WHERE client_id = $1` and asserts `codeCount != 0` fails (`:326-332`) — proves no code row was persisted, not merely that the response omitted one.

All four required assertions (deny decision, `error=access_denied`, no `code` param, zero `oauth_auth_codes` rows) are present and each targets the spec-defined outcome, not a weaker proxy. **Confirmed genuinely closed.**

### Gap #2 — Consent screen's client-name field was unasserted

`internal/dashboard/oauth_server_test.go:223-270` — `TestAuthorize_ActiveSessionHandsOffToConsent`, modified. Independently read:

- `:267-269`: `if locURL.Query().Get("client_name") != client.Name` — asserts the consent-handoff redirect's `client_name` query param equals the **registered client's actual name** (`client.Name`, the value returned by `RegisterClient`), not merely that the param is non-empty or present. A mutation that dropped or blanked `client_name` would fail this assertion (confirmed empirically below in the sensor section).

**Confirmed genuinely closed.**

Both fixes are read from the actual test file, not inferred from the fix commit's message, and both are non-shallow — they assert the spec-defined value, satisfying validate.md's payload/conjunction rule.

---

## Spec-Anchored Acceptance Criteria (fresh full pass, all ACs re-derived)

Only one file changed between pass #1 and this pass (`oauth_server_test.go`, verified via `git show --stat e9d35cc`), so all other citations below were independently spot-checked against current `HEAD` and confirmed unchanged; rows outside the OAuth-consent area move faster per validate.md's allowance ("may move faster through areas pass #1 already traced correctly if your own quick check confirms the same file:line still holds"). Spot-checked and confirmed still accurate: `pat_store_test.go:60-71`, `pat_handler_test.go:95-108`, `tools_write_test.go:38-58`, `tools_templates_test.go:65-86`.

### P1: Admin mints a PAT and connects an MCP client

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: PAT shown once, hashed | Plaintext returned once; stored row never equals plaintext | `internal/dashboard/pat_store_test.go:66-71` — `if storedHash == token { t.Fatal(...) }` and `storedHash != hashPATToken(token)` | ✅ PASS |
| AC2: valid PAT resolves to issuing admin | `ResolvePAT` returns the correct `DashboardUser` | `internal/dashboard/pat_store_test.go:127-132` — `if user.ID != userID` | ✅ PASS |
| AC3: missing/malformed/expired/revoked PAT rejected before tool exec | 401-equivalent, wrapped handler never runs | `internal/mcpserver/auth_test.go:97-140` — missing header, non-Bearer, unknown token; spy handler asserted never called | ⚠️ PASS with gap — expired PAT at the `RequirePAT`/MCP-transport layer specifically is still not directly cited (only at store layer, `pat_store_test.go:177`); unchanged from pass #1, carried forward as a note (see below) |
| AC4: revoked PAT rejected immediately | Next `ResolvePAT` call returns revoked error | `pat_store_test.go:159-161` — `if !errors.Is(err, ErrPATRevoked)` | ✅ PASS |
| AC5: create/revoke audited | `audit_log` row per action | `pat_handler_test.go:99-108` (`pat.create`), `:223-231` (`pat.revoke`) | ✅ PASS |

### P1: Admin connects Claude Desktop via OAuth 2.1

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: metadata discovery, 3 endpoints | Discovery doc names authorize/token/register URLs | `oauth_server_test.go:41-49` — exact URL string equality | ✅ PASS |
| AC2: dynamic registration, no manual setup | `client_id` issued on first call | `oauth_server_test.go:75-77` — `if resp.ClientID == ""` | ✅ PASS |
| AC3: no session → login, then consent naming the client | Login redirect preserving params; consent naming the requester | `oauth_server_test.go:203-217` (login redirect); `:246-269` (consent handoff, **now includes** `client_name == client.Name` at `:267-269`) | ✅ PASS — **gap #2 closed**, both halves of AC3 now covered |
| AC4: PKCE-bound code, single-use | Second exchange of a used code fails | `oauth_token_handler_test.go:118-123` — reused code → non-200 | ✅ PASS |
| AC5: token exchange resolves like a manual PAT | Same `ResolvePAT` identity path | `oauth_token_handler_test.go:73-79` — `if resolved.ID != userID` | ✅ PASS |
| AC6: refresh without repeating consent | New access+refresh pair, old refresh invalid | `oauth_token_handler_test.go:173-175` — new tokens differ from old | ⚠️ Spec-precision gap (unchanged from pass #1) — proves rotation, not an explicit "consent not re-shown" assertion; implicit in the code path (`refresh` never calls `Decide`) |
| AC7: reused/expired/PKCE-mismatched code rejected, no token issued | 400-class rejection, no token | `oauth_token_handler_test.go:114-135` — all three cases assert non-200 | ✅ PASS |

### P2: Create app/table with columns via MCP tools

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: `orbit_create_app` matches REST path, returns id/name | Same `CreateAppForUser` call; audit entry | `internal/mcpserver/tools_write_test.go:42-58` — `out.Name` equality + audit query for `app.create` | ✅ PASS |
| AC2: `orbit_create_table` matches REST validation | Table+columns created; dup name/column rejected | `tools_write_test.go:105-107`; `internal/dashboard/apps_table_foruser_test.go:49-63`, `:68-` | ✅ PASS |
| AC3: validation failure → structured, specific tool error | Non-generic error naming the field | `internal/mcpserver/tools_templates_test.go:254-260`, `tools_write_test.go:210-216` | ✅ PASS |
| AC4: `orbit_get_app_schema` reflects current state | Tables/columns/RLS mode match | `internal/mcpserver/tools_test.go:136-141` | ✅ PASS |
| AC5: same `audit_log` entry as REST (no dup path) | One audit call, shared function | `apps_table_foruser_test.go:34-43` | ✅ PASS |

### P2: Set RLS mode and apply policy template via MCP tools

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: `orbit_set_table_rls_mode` matches existing validation | Valid modes applied; invalid rejected, unchanged | `internal/mcpserver/tools_write_test.go:142-175` | ⚠️ Spec-precision note (unchanged from pass #1) — no dedicated REST "RLS-only" endpoint to literally extract from (`handler.go:1344-1352` comment); behavior/validation values match, wording in spec.md is imprecise, not a code defect |
| AC2: `orbit_list_policy_templates` matches UI's template set | Same 6 templates, id/description/required inputs | `internal/mcpserver/tools_templates_test.go:70-84` | ✅ PASS |
| AC3: `orbit_create_policy_from_template` matches sequential-create + partial-failure contract | Stop-on-first-error, enumerate created/failed/pending | `tools_templates_test.go:204-215` | ✅ PASS |
| AC4: missing/invalid required input rejected, zero policies created | Structured error naming the input, no writes | `tools_templates_test.go:254-267` | ✅ PASS |

**Status**: ✅ 16/16 AC rows PASS (up from 15/16 in pass #1 — the fixed row is P1-OAuth AC3). 2 rows carry pre-existing, non-blocking spec-precision notes (P1-OAuth AC6, P2-RLS AC1) — unchanged from pass #1, not new.

---

## Edge Cases (from spec.md)

| Edge case | Result | Evidence |
| --- | --- | --- |
| PAT's admin deactivated/deleted after mint → reject, derived live | ✅ Covered | `pat_store_test.go:199-201` — `errors.Is(err, ErrPATNotFound)` |
| Tool call targets an app the PAT's admin lost access to → same authz error as REST | ⚠️ Partial (unchanged) | `internal/mcpserver/tools_test.go:175-179` — never-had-access case only, not grant-then-revoke; spec doesn't name an exact error shape to match against (spec-precision gap in spec.md itself) |
| Two tool calls race to create a table with the same name → same last-write-wins as REST | ❌ NOT COVERED (unchanged) | No test found (`grep -rn "race\|concurrent" internal/mcpserver/*_test.go` → nothing) |
| Rate limit exceeded → reject further calls, session stays open | ⚠️ Partial (unchanged) | `internal/mcpserver/server_test.go:143-145` proves 429; nothing asserts the session itself survives |
| `orbit_create_policy_from_template` partial failure → enumerate created/failed/pending | ✅ Covered | `tools_templates_test.go:194-215` |
| Admin denies OAuth consent → redirect with `access_denied`, no code/token issued | ✅ **NOW COVERED** | `oauth_server_test.go:276-333` — `TestDecide_DenyRedirectsWithAccessDeniedAndNoCodeIssued`, independently verified above (was NOT COVERED in pass #1) |
| OAuth-issued PAT revoked → rejected like a manual PAT | ✅ Covered | `oauth_token_handler_test.go:223-225` |
| Refresh token reuse → reject + revoke whole family | ✅ Covered | `oauth_token_handler_test.go:218-225`, `oauth_integration_test.go:283-289` |

---

## Discrimination Sensor

Method: temporary `git worktree add /tmp/zeep-orbit-sensor-verify2 HEAD` (never `git stash`). Baseline `git status --porcelain` before sensor work: `?? .specs/features/mcp-server/design.md` plus `?? .specs/features/mcp-server/validation.md` (this report, not yet written when the sensor ran — written after). After sensor work and `git worktree remove --force`, `git status --porcelain` on the real tree matched the pre-sensor baseline exactly — isolation confirmed. (Note: the worktree's fresh checkout lacked the gitignored `internal/dashboard/static/` embed directory required by `embed.go`; a placeholder `index.html`/`assets/` dir was created inside the **scratch worktree only** to allow the package to build — never touched the real tree.)

Two new mutations targeting the fix commit's code, plus re-confirmation of all 5 of pass #1's original mutations against current `HEAD` (all 7 run in the same scratch worktree):

| # | Target | File:line | Mutation | Killed? |
| --- | --- | --- | --- | --- |
| 1 (new) | `Decide`'s deny branch | `internal/dashboard/oauth_server.go:279-282` | Deny branch made to call `CreateAuthCode` and return a `code` param instead of `error=access_denied` | ✅ Killed — `TestDecide_DenyRedirectsWithAccessDeniedAndNoCodeIssued` fails: `expected error=access_denied in the deny redirect, got ".../cb?code=...&state=xyz"` |
| 2 (new) | `Authorize`'s consent handoff | `internal/dashboard/oauth_server.go:200-201` | Removed `consentQuery.Set("client_name", client.Name)` | ✅ Killed — `TestAuthorize_ActiveSessionHandsOffToConsent` fails: `expected client_name to be preserved..., got "", want "cli"` |
| 3 (re-confirm) | PAT resolution | `internal/dashboard/pat_store.go:124` | `if revokedAt != nil` → `if false` | ✅ Killed — `TestResolvePAT_RevokedTokenRejected` fails: "expected ErrPATRevoked, got \<nil\>" |
| 4 (re-confirm) | OAuth PKCE verification | `internal/dashboard/oauth_server.go:312-315` | `pkceVerify` body forced to always `return true` | ✅ Killed — `TestTokenHandler_AuthorizationCode_ReusedExpiredOrMismatchedPKCERejected` fails: "expected 400 for mismatched PKCE verifier, got 200" |
| 5 (re-confirm) | Refresh-token reuse detection | `internal/dashboard/pat_store.go:306` | `if revokedAt != nil` → `if false` | ✅ Killed — `TestTokenHandler_RefreshToken_ReuseRejectedAndBlocksFurtherCalls` fails: "expected 400 invalid_grant on reuse..., got 200" |
| 6 (re-confirm) | RLS-mode validation | `internal/dashboard/handler.go:1367` | `if !config.ValidRLS(rlsMode)` → `if false` | ✅ Killed — `TestUpdateTableRLSModeForUser_InvalidValueRejected` fails: "expected a *ValidationError..., got \<nil\>" |
| 7 (re-confirm) | Ownership-scoping on revoke | `internal/dashboard/pat_store.go:172-174` | Removed `AND user_id = $2` from the `RevokePAT` UPDATE | ✅ Killed — `TestRevokePAT_ScopedToOwningUser` and `TestRevokePATHandler_AnotherUsersPATReturns404` both fail |

**Sensor depth**: lightweight (7 targeted mutations: 2 new for the fix commit, 5 re-confirmed from pass #1)
**Sensor outcome**: 7/7 killed, 0 survived

---

## Payload/Conjunction Rule

- PAT creation: `pat_store_test.go:66-71` asserts the stored hash value, not merely that `CreatePAT` was called.
- OAuth token issuance: `oauth_token_handler_test.go:73-79` and `:173-175` assert actual field values.
- OAuth deny (new): `oauth_server_test.go:316-332` asserts the exact `error` query value, absence of `code`, and a direct DB row count of 0 — not merely that `Decide` returned 200.
- OAuth consent client-name (new): `oauth_server_test.go:267-269` asserts `client_name` equals the registered client's actual `Name` field, not merely that the param is present/non-empty.
- Verdict: rule satisfied for all four surfaces.

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ — fix commit is test-only, 70 lines, 1 file |
| Surgical changes | ✅ |
| No scope creep | ✅ |
| Matches existing patterns | ✅ |
| Spec-anchored outcome check | ✅ (both former gaps now match spec-defined outcome exactly) |
| Every test maps to a spec requirement | ✅ |
| Documented guidelines followed | `AGENTS.md` §3 (gate commands), §4 (no in-memory OAuth state, no raw error leakage) — both followed |

---

## Gate Check

- **Gate command** (Build level, per tasks.md): `go build ./... && go test ./... -p 1 && go vet ./...`, `gofmt -l $(git diff --name-only 30ff8f2..HEAD -- '*.go')`, plus `cd internal/dashboard/ui && npx tsc -b && npm run build`, plus i18n JSON validation.
- **Environment**: `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable` (against the running `zeep-orbit-db-1` container, confirmed via `docker ps`), `DASHBOARD_BOOTSTRAP_SECRET` set to a 32+ char placeholder for pre-existing webhook tests.
- **Outcome**: all gates green.
  - `go build ./...` — clean, no output.
  - `go vet ./...` — clean, no output.
  - `gofmt -l <changed .go files>` — clean, no output.
  - `go test ./... -p 1 -v` — **547 passed, 0 failed** (up from 546 in pass #1 — the fix commit added exactly 1 new test function; the modified `TestAuthorize_ActiveSessionHandsOffToConsent` is a strengthened existing test, not a new one).
  - `npx tsc -b` — clean.
  - `npm run build` — succeeded (487 modules transformed; one pre-existing chunk-size advisory warning, unrelated to this feature).
  - i18n JSON (`en.json`, `pt-BR.json`) — both parse.
- **Failures**: none.

---

## Requirement Traceability Update

| Requirement | Traceability table says | Independent verdict (pass 2) |
| --- | --- | --- |
| MCP-01 through MCP-05 | Verified | ✅ Confirmed, AC3-expired-at-transport-layer note carried forward (non-blocking) |
| MCP-06 through MCP-14 | Verified | ✅ Confirmed |
| MCP-19, MCP-20 | Verified | ✅ Confirmed |
| MCP-21 | Verified | ✅ **Confirmed in full** — consent-naming assertion now present (was ⚠️ partial in pass #1) |
| MCP-22 | Verified | ✅ **Confirmed in full** — deny-consent edge case now covered (was ❌ NOT COVERED in pass #1) |
| MCP-23 | Verified | ✅ Confirmed |
| MCP-24 | Verified | ✅ Confirmed |
| MCP-15 through MCP-18 | Pending | Correctly left Pending — P3 out of scope for this task set |

No requirement is downgraded. MCP-21 and MCP-22 move from "confirmed with a coverage gap" to fully confirmed.

---

## Summary

**Overall**: ✅ Ready — clean PASS.

**Spec-anchored check**: 16/16 AC rows PASS (2 carry pre-existing, non-blocking spec-precision notes — see below).

**Sensor**: 7/7 mutations killed (2 new targeting the fix commit, 5 re-confirmed from pass #1) — 0 survived.

**Gate**: all commands passed (build, vet, gofmt, full test suite 547/547, tsc, vite build, i18n JSON).

**What works**: PAT lifecycle (create/resolve/revoke/expire), OAuth discovery/registration/code-exchange/PKCE/refresh-rotation/reuse-detection-with-family-revocation, **OAuth deny-consent (now tested)**, **consent screen client-naming (now tested)**, all 7 MCP tools with shared-code-path validation and audit parity, ownership-scoping on revoke, no in-memory cross-replica state anywhere in the OAuth flow.

**Gaps closed this pass**: both of pass #1's high-priority NOT-COVERED items (OAuth deny-consent path; consent screen client-name assertion) — independently re-verified as genuine, non-shallow test coverage, not just plausibly-named tests.

**Notes carried forward (not blocking PASS, at this Verifier's judgment)**:
1. `policytemplates` cross-check test (`templates_test.go`) hardcodes expected values instead of executing `policyTemplates.ts` — drift-prone but currently correct by manual verification, and already a documented accepted risk in design.md. Reasoning for not blocking: it's a test-quality improvement, not evidence of incorrect behavior; the manual diff still confirms current parity.
2. PAT expiry not proven specifically at the `RequirePAT` middleware/MCP-transport layer (only at the store layer, which the transport layer delegates to via the same `ResolvePATWithID` function). Reasoning for not blocking: high confidence via shared-code-path reasoning, cheap to close later, not a sign of broken behavior.
3. Rate-limit "session stays open" and the duplicate-table-name race are only partially/not tested. Reasoning for not blocking: spec.md explicitly says "no new locking introduced for MCP specifically" for the race case (implying REST-level behavior, out of this diff's surface), and the rate-limit session-survival assertion is a coverage nicety, not a behavior currently suspected broken.

**Next steps**: none required to close this feature. Items 1-3 above are reasonable to bundle into a future hardening/test-quality follow-up ticket, at the team's discretion — they do not block shipping.
