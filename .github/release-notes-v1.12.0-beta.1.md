## v1.12.0-beta.1

> **Pre-release (beta).** It is published only under its own version: `latest`, `1.12` and the GitHub "Latest release" stay on v1.11.1, so nothing upgrades to it by accident. Use it in a controlled environment and report what you find; the stable v1.12.0 follows after real use.
>
> **Install it by version, and set the image tag explicitly.** The chart's default `image.tag` is `latest`, which is still v1.11.1:
>
> ```bash
> docker pull ghcr.io/zeeplabs/zeep-orbit:1.12.0-beta.1
> helm upgrade --install zeep-orbit <chart-1.12.0-beta.1.tgz from this release> --set image.tag=1.12.0-beta.1
> ```
>
> The builder and registry-proxy images use the chart `appVersion` by default (`1.12.0-beta.1`). The beta chart is attached to this release and is not in the Helm repository, so a plain `helm install` never picks it up.
>
> The dashboard of a beta install shows "New version available: v1.11.1", because the update banner only compares for equality. Ignore it.

Minor release that adds the **AI Frontend Builder**: describe a frontend in a chat, preview it live against a backend app, and publish it. It ships with a chat-style home, planning, cost accounting and two new services (the builder and an npm registry proxy). **The builder is off by default.** An installation that does not set `BUILDER_URL` behaves as in v1.11.0, apart from the text changes listed below.

### Added

- **AI Frontend Builder.** An agent asks questions, proposes a build plan you approve, then builds the frontend in milestones. The preview is served through Orbit and publishing goes through your connected Render account. Superadmins configure it under **Integrations → AI Builder** (provider, model, token and cost budgets, persona and rules, skills, GitHub templates).
- **Chat home and My projects.** When the builder is available, `/dashboard/` opens a chat-style home and **My projects** lists your sessions and backend apps. A switch moves between this simple interface and the advanced one (the existing screens, unchanged).
- **Cost accounting.** Every model call is recorded in `ai_usage_ledger` with its token classes and cost in USD. The chat shows the cost of each step, **AI usage** reports a date range, and budgets cap each session and each user per month. Providers: `openai`, `openai_compatible` (with a base URL) and `anthropic` (with prompt caching). OpenAI and Anthropic models and prices come from a catalog shipped with Orbit.
- **Deployment.** Docker Compose `builder` profile, Helm `builder.enabled` and a Kustomize `k8s/builder` component. CI publishes two extra images next to the main one, `<repository>-builder` and `<repository>-registry-proxy`.

### Fixed and hardened

- Token and cost caps now apply to providers that report no usage. A turn that keeps crashing no longer blocks its session. A model call is no longer retried when it may already have been answered and billed.
- Environment files are left out of a publish in any letter case. The settings API refuses `publish_rls_gate: "block"` and `backend_approval_mode: "auto"`.
- The builder has no route to Postgres or the internet, its shared secret and the registry tokens are not exposed to build steps, the npm proxy forwards only metadata and tarballs, and disk use on the shared volume is capped.
- Dashboard: signing in from a link returns to that link, and unsaved-changes prompts cover the builder settings and the mode switch.

### Upgrade notes

- **Nothing changes unless you turn the builder on.** It needs `BUILDER_URL` and `BUILDER_SHARED_SECRET` (at least 16 bytes), the `builder` Compose profile or `builder.enabled` in Helm, and a configured provider and budgets (otherwise `GET /dashboard/api/builder/status` answers `available: false`). See `docs/docs/ai-frontend-builder.md`.
- **Behavior changes, only with the builder on:** `planning_mode` defaults to `required` (new sessions start with questions and a plan to approve; set **Planning** to `suggested` or `off` for the old behavior); `openai` and `anthropic` accept only catalog models and costs are in USD, and a provider left on a model outside the catalog makes the builder unavailable (`model_not_in_catalog`) until a superadmin picks one; changing the `openai_compatible` base URL without a new key drops the stored key.
- **Landing change:** for users in simple mode on an installation where the builder is available, login and `/dashboard/` now open the chat home instead of `/dashboard/apps`. Advanced-mode users and installations without the builder still land on `/apps`.
- **Docker Compose:** the `zeep` service now joins `default` and `builder_internal`. Without the profile nothing else runs.
- **Text only:** Orbit is no longer described as a Backend-as-a-Service; it is described as a self-hosted platform for building apps with AI on a real backend.
- **Migrations run automatically at boot** and are additive (`builder_*` tables and columns, `ai_usage_ledger`, `ai_providers.base_url`).

### Known limitations

- **The builder runs code written by an AI, so treat it as untrusted code.** All sessions share one builder container, user and volume, so there is no isolation between hostile users: run it for people you trust.
- Code the agent writes (`npm run lint`, the Vite config) still executes inside the builder container.
- Egress from the registry proxy can still be abused through DNS; this residual channel is documented.
- NetworkPolicy needs a CNI that enforces it. Isolation on Easypanel and Swarm is unverified.
- The Compose network isolation and the non-dumpable builder process were validated by tests and review, not in a long-running production use. Try them in a controlled environment first.
