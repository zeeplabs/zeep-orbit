# Dashboard handlers, REST routes, MCP tools

## The `*ForUser` pattern (Shared Operation Functions)

REST handlers and MCP tools are two transports over the same operation functions in `internal/dashboard`. Named explicitly as "Shared Operation Functions" in `internal/mcpserver/server.go:34` (design lives in `.specs/features/mcp-server/design.md`). Key methods, all in `internal/dashboard/handler.go` unless noted:

- `CreateAppForUser` (`:945`), `UpdateAppForUser` (`:1554`)
- `CreateAppTableForUser` (`:1237`), `UpdateTableRLSModeForUser` (`:1471`)
- `AddTableColumnForUser` (`:1597`), `AddTableIndexForUser` (`:1673`)
- `AddColumnForeignKeyForUser` (`:1749`), `RemoveColumnForeignKeyForUser` (`:1833`)
- `UpdateColumnEnumValuesForUser` (`:1906`)
- `CreateTablePolicyForUser` (`:2144`)
- `ListAppsForUser`, `GetAppSchemaForUser` (`app_schema.go:13,65`)
- `CreateWebhookForUser`, `SaveEventMappingForUser` (`webhooks_store.go:307,348`)

When adding a new capability: write (or extend) one `*ForUser` method, then wire a thin REST handler and/or a thin MCP tool on top of it — don't hand-roll validation/provisioning/audit logic separately in either transport.

## REST routes (`internal/server/server.go`)

- `/dashboard/*` — dashboard's own API, mounted at `:213`.
- `/{app}/auth/*` (`:352-363`): `providers` (GET); `register`/`login`/`token/refresh` (POST, rate-limited via `ah.RateLimit`); `refresh` (POST); `logout`/`me` (GET/PATCH, behind `AuthJWTMiddleware`); `google/login`/`google/callback` (GET).
- `/{app}/files/*` (`:368-377`, behind `appRateLimit` + `JWTMiddleware`): upload (POST), list (GET), get/download/signed-url/delete by `{id}`.
- `/{app}/health` (`:379`).
- `/{app}/{table}` (`:381-386`): GET list / POST create, rate-limited + JWT.
- `/{app}/{table}/{id}` (`:388-395`): GET/PUT/PATCH/DELETE.
- `/hooks/{webhookId}/{token}` — inbound webhook delivery, registered via plain `HandleFunc` rather than a verb-specific route (`:169`; see `.specs/features/.../design.md` referenced at `:155` for why).

Query filter operators (`internal/query/builder.go`, `operatorMap` at `:72-81`): `eq.`→`=`, `ne.`→`!=`, `gt.`→`>`, `gte.`→`>=`, `lt.`→`<`, `lte.`→`<=`, `like.`→`LIKE`, `ilike.`→`ILIKE`; `in.` is handled separately (comma-split, `:130-140`). `BuildList` (`:85`) rejects any filter key not in the table's schema with `"query: unknown field in filter"` (`:126-128`); an unrecognized operator prefix errors listing the supported ones (`:154`).

## MCP tools (`internal/mcpserver`)

`ToolDeps{Pool *db.Pool; DashH *dashboard.Handler}` (`tools.go:34-37`) — the same `*dashboard.Handler` instance the REST routes use (comment at `tools.go:26-29`). `RegisterTools(server, deps)` (`tools.go:66`) registers, in order: `registerReadTools`, `registerWriteTools`, `registerTemplateTools`, `registerAdvancedPolicyTool`, `registerAppConfigReadTools`, `registerAccessReadTools`, `registerOperationalReadTools`, `registerAppConfigWriteTools`, `registerOperationalWriteTools` (`tools.go:67-75`). Each tool calls a `deps.DashH` `*ForUser` method directly — no separate code path.

Error handling: any internal failure becomes a fixed `errInternal` (`tools.go:38-42`), logged server-side via `internalErr` (`tools.go:57-60`) — raw `err.Error()` never reaches the caller (AGENTS.md §4, cited literally in comments at `tools.go:6,41,51,172,742` and elsewhere). Typed errors like `provisioner.ValidationError` are the documented exception — `mapWriteError` surfaces those verbatim instead of collapsing them to the generic message.

Auth: `RequirePAT` (`internal/mcpserver/auth.go:46`) is the PAT-bearer-token equivalent of `dashboard.RequireAuth`'s cookie-session auth (`auth.go:37`). `NewHandler` (`server.go:35-47`) wraps, from innermost to outermost: `mcp.NewStreamableHTTPHandler(..., Stateless: true)` → `rl.MiddlewareKeyedBy(patIDKey)` → `RequirePAT(pool)`. `Stateless: true` is deliberate — this service runs multiple replicas behind a non-sticky load balancer, so a session id tied to one replica's in-memory server would break on the next request (`server.go:21-28`), consistent with PATs never being cached in memory anywhere else in the codebase.

`ServerOptions.Instructions` (`internal/mcpserver/instructions.go`, wired in `server.go`'s `mcp.NewServer` call) carries a short natural-language brief sent to every connecting client on `initialize` — keep it in sync with the tool categories above if you add/remove a `register*Tools` group, but keep it short; it's not a substitute for `tools/list`.

## Frontend

`internal/dashboard/ui` — React/Vite/TS, built and embedded into the Go binary via `go:embed` (`internal/dashboard/embed.go`/`static`). Not covered in depth here; see `internal/dashboard/ui`'s own conventions when working there.
