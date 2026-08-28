# Testing conventions

## Running the suite

`go build ./...`, `go test -p 1 -parallel 4 ./...`, `go vet ./...`, `gofmt -l <changed files>` (AGENTS.md §3). The `-p 1 -parallel 4` isn't optional style — `internal/dashboard` and `internal/mcpserver` both run integration tests against the same `TEST_DATABASE_URL`, stepping on each other's `zeep_system` rows under Go's default cross-package parallelism (deadlocks / spurious FK violations, not a real bug). This is what CI runs (`.github/workflows/reusable-ci.yml`) — match it locally or you'll see failures that don't reproduce in CI's own log, or vice versa.

## Pool setup — no shared global helper

There is no single `newTestPool`/`testDB` helper shared across packages. Every `*_test.go` file that needs a database defines its own local helper following the same convention — e.g. `appTokensTestPool(t)` in `internal/dashboard/app_tokens_foruser_test.go:24-40`, whose own comment (`:4-5`) points at `table_policies_handler_test.go` as the same approach. The repeated shape:

1. `os.Getenv("TEST_DATABASE_URL")`, `t.Skip("TEST_DATABASE_URL not set")` if empty (seen across dozens of files, e.g. `internal/auth/handler_test.go:32`, `internal/dashboard/ai_build_chat_handlers_test.go:38-40`).
2. Drop `zeep_system` `CASCADE` to reset state.
3. `ProvisionZeepSystem(ctx, pool)` to recreate the baseline schema (`app_tokens_foruser_test.go:33-38`).

Copy the nearest existing test file's helper rather than inventing a new pattern or trying to extract a shared one — the convention is intentionally file-local so each test file's setup stays self-contained and greppable in place.

## Webhook tests need an extra env var

Anything touching `CreateWebhookForUser`/webhook token encryption requires `WEBHOOK_TOKEN_ENCRYPTION_KEY` (or `DASHBOARD_BOOTSTRAP_SECRET`) set in the test environment — its absence fails with `crypto: neither WEBHOOK_TOKEN_ENCRYPTION_KEY nor DASHBOARD_BOOTSTRAP_SECRET is set`, unrelated to whatever you changed. Set it locally, e.g. `WEBHOOK_TOKEN_ENCRYPTION_KEY=$(openssl rand -base64 32) go test ./internal/mcpserver/...`, before concluding a webhook-adjacent test failure is a real regression.
