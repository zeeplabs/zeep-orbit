## Highlights

- **MCP server** — Zeep Orbit now exposes a Model Context Protocol endpoint (`/dashboard/mcp`), so an AI coding agent (Claude Code, Claude Desktop, Cursor, Codex, OpenCode) can create and manage apps, tables, RLS modes, and row policies through the same operations the dashboard's REST API already performs — one tool per existing endpoint, not a parallel code path. Two auth methods: **Personal Access Tokens** (bearer, created/listed/revoked from the dashboard) for CLI-style clients, and **OAuth 2.1 with PKCE + dynamic client registration** (RFC 7591) for clients like Claude Desktop that have no way to paste a static token.
- **A dedicated "MCP" page in the dashboard** (Deployment section of the sidebar) walks through connecting each supported client, with the live endpoint URL and copy-pasteable, syntax-highlighted config for each. Personal access token management (create, list, revoke) moved here from its previous home behind an unlabeled key icon in the sidebar footer.
- **A superadmin-only "Registered OAuth clients" section** lists every client that has dynamically self-registered against this instance's OAuth endpoint (registration is unauthenticated by RFC 7591 design, so this would otherwise be invisible) and lets a superadmin delete one — cascading to revoke every authorization code and access/refresh token pair that client was ever issued.

## Security (OAuth 2.1)

This is the first release the MCP server and OAuth 2.1 flow ship in, hardened through three rounds of independent review before release:

- Registered `redirect_uris` are restricted to `https://` (or `http://` on loopback, for native/CLI clients per RFC 8252) — an unrestricted scheme would let a maliciously-registered client run script on the dashboard origin via the consent screen's redirect.
- Authorization codes are single-use and PKCE-bound; refresh tokens rotate with reuse detection (a replayed, already-superseded token revokes its entire token family).
- `client_id` is verified against the token's issuing client at every token exchange (`authorization_code` and `refresh_token` grants alike) — not just accepted and ignored.
- The consent flow correctly resumes after a login (password or Google SSO, including a first-time Google setup step) instead of dropping the admin back on the dashboard home.
- A new optional `ORBIT_PUBLIC_URL` env var pins the base URL the OAuth metadata document advertises, instead of trusting the request's `Host`/`X-Forwarded-Proto` headers unconditionally — set it if Orbit runs without a reverse proxy that validates those headers.

## Upgrade notes

- No manual migration steps. `ORBIT_PUBLIC_URL` is optional; unset, behavior is unchanged from a request-derived base URL.
- Nothing about existing apps, tables, RLS modes, or webhooks changes in this release.
