## Highlights

Patch release. No new features — a security fix, an i18n cleanup pass, a dead-code removal, and a rate-limiter hardening pass that came out of pre-release review.

## Security

- **Google OAuth callback error leak.** The per-app Google OAuth callback was leaking the raw token-exchange error and the app's configured `redirect_url` in a 500 response. The client now gets a fixed generic message; the real error is logged server-side only.
- **Webhook rate-limiter hardening.** The public `/hooks/{webhookId}/{token}` route's rate limiter (fixed last version to key on the resolved webhook id instead of the raw URL param) had two remaining gaps found in pre-release review: a flood against one fixed nonexistent id had no budget to hit since resolution happens before the limiter runs, and the per-webhook budget was charged before token verification, letting anyone who learns a real `webhookId` (visible in the dashboard URL) burn a legitimate subscription's quota with garbage tokens. Added a coarse per-IP guard ahead of the database lookup, moved the delivery budget to charge only once a token verifies, and gave invalid-token attempts their own separate, smaller budget.

## Fixes

- Several user-facing strings still in Portuguese despite the dashboard's English default: the platform-wide Google OAuth error page, the CLI's `serve`/`status` command descriptions, and a registry config-validation error string.
- `Makefile`, `README.md`, `CONTRIBUTING.md` (all 4 languages) documented running the Go test suite without the flags CI already uses to avoid spurious deadlocks between two packages sharing one integration-test database.
- Personal-access-tokens e2e suite's per-test cleanup was deleting every PAT on the shared test account instead of just its own.

## Removed

- Dead `registry.Registry.Load` config-loading code, left over from the removed YAML-config-apply flow.

## Upgrade notes

No breaking changes, no migrations required. Standard upgrade via Helm chart or Docker image bump.

## Links

- Full changelog: [CHANGELOG.md](https://github.com/zeeplabs/zeep-orbit/blob/main/CHANGELOG.md)
