## Highlights

Patch release. Fixes the per-app generated API documentation (OpenAPI/Swagger), which omitted an app's Google OAuth routes even when Google sign-in was fully configured.

## Fixes

- **Google OAuth routes missing from generated API docs.** The per-app documentation generator only checked the email/password auth flag when deciding which auth routes to document; it never checked whether Google sign-in was enabled, which lives in a separate field. `GET /{app}/auth/google/login` and `GET /{app}/auth/google/callback` always worked correctly — they just never appeared in the generated spec for the app. They now do, whenever Google sign-in is enabled and fully configured (a provider with no client ID configured, which would fail at runtime, is correctly still omitted).
- **`GET /{app}/auth/providers` now always documented.** This route exists for every app regardless of which auth providers are enabled, but was previously only documented alongside Google sign-in — an email-only (or no-auth-provider) app was missing it from its own generated docs.

## Upgrade notes

No breaking changes, no migrations required, no runtime behavior changed — documentation-only fix. Standard upgrade via Helm chart or Docker image bump.

## Links

- Full changelog: [CHANGELOG.md](https://github.com/zeeplabs/zeep-orbit/blob/main/CHANGELOG.md)
