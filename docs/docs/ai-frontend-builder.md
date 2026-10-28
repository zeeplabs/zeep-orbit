---
sidebar_position: 8
---

# AI Frontend Builder

The AI Frontend Builder lets a dashboard user describe a frontend in a chat and see it running in a live preview, already wired to one of their Orbit backend apps. When the result is good enough, the user publishes it through the existing frontend-app pipeline (a private GitHub repository plus a Render static site).

It is **off by default**. Nothing is deployed and no route does any work until you set `BUILDER_URL` on Orbit, point it at a builder service, and a superadmin enables it in the dashboard.

:::caution
The builder runs code that an LLM wrote, and runs `npm install` on dependencies the LLM chose. That is untrusted code by definition. Read [Isolation model](#isolation-model) and [Deployment caveats](#deployment-caveats) before enabling it on a shared or production installation.
:::

## How it works

Three pieces, all built from this repository:

| Piece | Binary | Network reach | Secrets it holds |
|-------|--------|---------------|------------------|
| **Orbit** (existing, N replicas) | `cmd/zeep` | Postgres, the LLM provider, GitHub, Render, the builder | LLM key, GitHub App key, Render key, `BUILDER_SHARED_SECRET` |
| **Builder** (new, one replica) | `cmd/zeep-builder` | Only the registry proxy. Reached only by Orbit. | `BUILDER_SHARED_SECRET` only |
| **Registry proxy** (new, one replica) | `cmd/zeep-registry-proxy` | The configured npm upstream registries over HTTPS | Optional private-registry tokens |

- **The agent loop runs in Orbit, not in the builder.** Orbit assembles the prompt, calls the LLM, and executes each tool call. The builder is only a tool executor: it reads and writes files in a per-session git working copy, runs a fixed set of build steps, and serves the last successful build as the preview.
- **The builder has no internet, no database, and no LLM key.** It has no route to Postgres, makes no LLM calls, and holds no secret other than the shared HMAC secret it uses to authenticate Orbit.
- **The registry proxy is the builder's only way out.** It forwards `GET` and `HEAD` requests to the npm upstreams you configure and injects private-registry credentials, so the builder never sees them.
- **`docker.sock` is never mounted** in any Orbit or builder container, in any shipped manifest.
- **Turns are queued in Postgres.** A message creates a turn. Any Orbit replica can claim it (row lease, `FOR UPDATE SKIP LOCKED`), so a session survives a replica restart and is never run by two replicas at once. The browser polls an append-only event log with a cursor; there is no streaming.
- **Everything the agent can do is a fixed tool.** `list_dir`, `read_file`, `write_file`, `edit_file`, `run_build` (`install`, `typecheck`, `lint`, `build` only), `get_build_logs`, `orbit_get_app_schema`, `orbit_list_auth_providers` and `propose_backend_change`. There is no shell tool.
- **Backend changes need confirmation.** The two Orbit read tools are scoped to the session's linked backend app and run with the session owner's permissions. When the agent wants a new table, column or policy, it emits a proposal and the turn pauses. The user confirms or rejects it; a confirmed proposal is applied through the same path as "Edit with AI" (same validation and RBAC, audit origin `ai_builder`) and requires write access on the backend app.
- **One commit per turn.** After each turn the builder commits the changes, and Orbit stores an incremental `git bundle` of that commit in Postgres (`builder_snapshots`). The builder volume is a cache: if it is lost, Orbit restores the session from those snapshots.
- **Reverting** restores the working copy to the state after an earlier turn as a *new* commit (history is not rewritten), rebuilds the preview, and makes no model call, so it spends no tokens.
- **Sessions are owner-only.** A superadmin cannot read other users' sessions. Using the builder requires at least viewer access on the backend app; confirming a backend change requires editor or admin.

## Enabling it

Orbit reads three variables on its side (the builder daemon and the proxy have their own, listed under [Environment variables](#environment-variables)).

| Variable | Default | Behavior |
|----------|---------|----------|
| `BUILDER_URL` | empty | Base URL of the builder, for example `http://builder:8081`. **Empty means the feature is off**: no worker goroutine and no sandbox client are created, and every builder route except `GET /dashboard/api/builder/status` answers `503 {"error":"builder not configured"}`. |
| `BUILDER_SHARED_SECRET` | none | HMAC key shared by Orbit and the builder. Used to sign Orbit-to-builder requests and the preview token. At least 16 bytes. Required when `BUILDER_URL` is set: Orbit refuses to start if it is missing or shorter, and the builder daemon refuses to boot the same way. |
| `BUILDER_WORKER_CONCURRENCY` | `2` | Maximum number of turns one Orbit replica runs at once. Unset, empty, non-numeric or less than 1 falls back to `2`. |

Use the same `BUILDER_SHARED_SECRET` value on Orbit and on the builder. Generate it yourself, for example with `openssl rand -hex 32`, and never commit it.

### Docker Compose

The repository's `docker-compose.yml` defines the `builder` and `registry-proxy` services under the `builder` profile, and builds their images from `Dockerfile.builder` and `Dockerfile.registry-proxy`. Without the profile, only `zeep` and `db` start.

In `.env`:

```bash
BUILDER_URL=http://builder:8081
BUILDER_SHARED_SECRET=<at least 16 bytes, from your secret store>
```

Then:

```bash
docker compose --profile builder up -d
```

The shipped topology:

- `builder` is attached **only** to `builder_internal`, a network declared `internal: true` (no gateway, no outbound route). It cannot reach `db:5432` or the internet.
- `registry-proxy` is attached to `builder_internal` and to `egress` (a normal network). It is the only service on `egress`.
- `zeep` is attached to `default` and `builder_internal`, so it can reach the builder.
- The builder runs as uid 1000 with a read-only root filesystem, all capabilities dropped, `no-new-privileges`, `pids_limit: 512`, `cpus: 2` and `mem_limit: 4g`. Workspaces live on the `builder_work` volume.

Optional variables for the proxy are read from `.env` (`REGISTRY_PROXY_UPSTREAMS`, `REGISTRY_PROXY_TOKEN_DEFAULT`, `REGISTRY_PROXY_TOKEN_COMPANY`).

### Helm

`builder.enabled` is `false` by default. With it off, the chart renders no builder object and the Orbit pods get no `BUILDER_*` variable.

```yaml
# values.yaml
builder:
  enabled: true
  # Pick one. Prefer an existing Secret over a plain value.
  existingSecret: "orbit-builder"   # a Secret with the key BUILDER_SHARED_SECRET
  # sharedSecret: "<at least 16 bytes>"
```

When `builder.enabled` is `true`, the chart renders a builder Deployment (one replica, `Recreate` strategy), a PersistentVolumeClaim (default `10Gi`, `ReadWriteOnce`), a registry-proxy Deployment and Services, and, when `builder.networkPolicy.enabled` is `true` (the default), the NetworkPolicies described below. It also sets `BUILDER_URL` and `BUILDER_SHARED_SECRET` on the Orbit pods. The chart fails to render if neither `builder.sharedSecret` nor `builder.existingSecret` is set.

Relevant values (see `charts/zeep-orbit/values.yaml` for the full list):

| Value | Default | Notes |
|-------|---------|-------|
| `builder.image.repository` | `ghcr.io/zeeplabs/zeep-orbit-builder` | Tag defaults to the chart `appVersion`. |
| `builder.registryProxy.image.repository` | `ghcr.io/zeeplabs/zeep-orbit-registry-proxy` | Tag defaults to the chart `appVersion`. |
| `builder.resources.limits.memory` | `4Gi` | See [Sizing](#sizing). |
| `builder.resources.limits.cpu` | `2` | |
| `builder.persistence.size` | `10Gi` | |
| `builder.registryProxy.upstreams` | `https://registry.npmjs.org` | Becomes `REGISTRY_PROXY_UPSTREAMS`. |
| `builder.registryProxy.tokenDefault`, `tokenCompany` | empty | Stored in the chart Secret and exposed to the proxy as `REGISTRY_PROXY_TOKEN_DEFAULT` and `REGISTRY_PROXY_TOKEN_COMPANY`. |

### Kustomize

`k8s/builder` is a Kustomize **Component**. A Component cannot be applied on its own, and Kustomize rejects an overlay that lives inside the base directory, so use it from an overlay **outside** `k8s/`:

```yaml
# my-overlay/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../k8s
components:
  - ../k8s/builder
```

```bash
kubectl apply -k my-overlay
```

The Component adds the builder, the registry proxy and the NetworkPolicies, and patches the `zeep-orbit` Deployment with `BUILDER_URL` and `BUILDER_SHARED_SECRET`. Create the Secret before applying it, outside GitOps (`k8s/builder/secret.example.yaml` is a template, not a value to commit):

```bash
kubectl create secret generic zeep-orbit-builder-secret \
  --from-literal=BUILDER_SHARED_SECRET="$(openssl rand -hex 32)"
```

The manifests reference `ghcr.io/zeeplabs/zeep-orbit-builder:latest` and `ghcr.io/zeeplabs/zeep-orbit-registry-proxy:latest`. Pin the tags in your overlay.

### Images

The release workflow publishes two images next to the main one, with the same tag scheme: `ghcr.io/<repository>-builder` and `ghcr.io/<repository>-registry-proxy` (for this repository, `ghcr.io/zeeplabs/zeep-orbit-builder` and `ghcr.io/zeeplabs/zeep-orbit-registry-proxy`). The main image name and tags are unchanged. Whether the first tagged release actually publishes all three images has not been checked yet.

## Superadmin configuration

After deployment, a superadmin completes setup in the dashboard, under **Integrations**. Until then the builder reports itself as unavailable and nobody can start a session.

1. **Model provider** (the **AI** tab). The builder uses a provider row of the existing AI provider configuration. Two kinds are supported:
   - `openai`: the OpenAI API, with an API key and a model.
   - `openai_compatible`: any server that speaks the OpenAI Chat Completions API (for example Ollama, vLLM, Azure OpenAI or a gateway). It needs a **base URL** and a model. The API key is optional for this kind.

   The key is encrypted at rest and never returned by the API (only `has_key`). Updating the model without sending the key keeps the stored key.

   The base URL must be an absolute `http` or `https` URL without credentials, query string or fragment. Private and loopback hosts are allowed (an Ollama container on an internal network is a supported setup); link-local and cloud-metadata addresses are rejected. The check is made when you save and again when the connection is made, on the IP address actually dialed, so a hostname that resolves to a blocked address is also refused. If `HTTP_PROXY` or `HTTPS_PROXY` is set in Orbit's environment, the connection goes to that proxy and the check sees the proxy's address, not the target's.

2. **Builder settings** (the **AI Builder** tab).

   | Setting | Default | Allowed range |
   |---------|---------|---------------|
   | Enabled | off | |
   | Model provider | `openai` | `openai` or `openai_compatible` |
   | Context window (tokens) | `128000` | 4096 to 2000000 |
   | Session token budget | `0` (unset) | 0 to 10^12 |
   | Monthly token budget per user | `0` (unset) | 0 to 10^12 |
   | Max steps per turn | `40` | 1 to 200 |
   | Max fix iterations (failed builds per turn) | `3` | 1 to 20 |
   | Max seconds per turn | `600` | 10 to 3600 |

   Token budgets count input and output tokens. **Both budgets must be greater than zero.** With either one at `0`, the builder reports itself unavailable (`budget_not_configured`) and **Create with AI** is disabled; sending a message to an existing session is refused the same way. If a cap is set back to `0` while a turn is already running, that turn's next model call is refused and the turn ends with a `budget_exceeded` event. The budget is checked before each model call, against what was already recorded, so the call that crosses the limit is the last one. The monthly budget counts the calendar month in UTC. A settings change applies to the next turn without restarting Orbit.

3. **Persona and rules** (the **AI Builder** tab). Company text added to every session, after Orbit's own core prompt. The core prompt takes precedence: a company rule cannot switch off confirmation of backend changes, key isolation or app scoping. Persona and rules together are limited to 8000 characters. Every save that changes the content creates a new version (author and time recorded, older versions kept and visible in the history). A session uses the version that was active when it was created; later edits apply only to sessions started afterwards. Saves are audit-logged with sizes and version ids, not the text.

4. **Base templates** (the **AI Builder** tab). A session starts from the built-in default template (React, Vite, TypeScript and the Orbit TypeScript client), which is always available, or from a registered GitHub template that a superadmin has marked as available for the builder. Inactive templates are hidden from users even when enabled. A template can document its own conventions in an `ORBIT_AGENT.md` file at its root, which is added to the prompt, and can ship its own lint configuration: `run_build lint` runs it and the agent receives the violations. Each session records the template and version it started from.

5. **Skills** (the **AI Builder** tab). A skill is a short markdown guide the agent loads on demand, the way coding agents use `SKILL.md` files. The agent always sees a one-line index of the enabled skills (name and description) in its prompt and loads a skill's full text with the read-only `load_skill` tool when the description matches the task. With no enabled skill there is no skills section in the prompt and the agent is not offered `load_skill`.

   - **Built-in skills** ship inside Orbit (names start with `orbit-`). They are listed in the tab with a lock: you can turn them on or off, but not edit or delete them, and the server refuses any attempt (`409`, `skill_builtin_readonly`). Six ship today: `orbit-frontend-design`, `orbit-accessibility`, `orbit-react-patterns`, `orbit-responsive-layout`, `orbit-forms-and-states` and `orbit-crud-screens`. They are written for this builder's stack (React, Vite, TypeScript, plain CSS, `HashRouter`, the builder's tools), and the design skill yields to the company design system and the template guide. Two of them adapt open-source skills (Apache-2.0 and MIT); the attributions are in `internal/agent/skills/builtin/NOTICE.md`. The index space reserved for built-ins is 2500 characters, so a future built-in cannot push your own skills out of the agent's view.
   - **Custom skills** are uploaded by a superadmin as a `SKILL.md` file (a file picker or pasted text). The file starts with a frontmatter block between `---` lines with `name` and `description` (and optionally `license`), followed by the markdown body. Every other frontmatter key (`allowed-tools`, `hooks`, `model`, `version`, `source`, ...) is ignored and listed as a warning after the upload: a skill never changes the agent's tools or permissions. Uploading a name that already exists creates a new version; identical content creates nothing. Versions are kept and can be viewed; a custom skill can be turned off or deleted (soft delete, the history stays for audit, and the name can be uploaded again as a new skill). Names starting with `orbit-` are reserved.
   - **Limits** (fixed in code, no setting): name up to 64 characters of lowercase letters, digits and single hyphens; description up to 1024 characters (the limit of the Agent Skills format), one line; body up to 24 KiB; file up to 64 KiB; a turn may load at most 5 different skills; the skills index may hold up to 8000 characters in total, and an upload or an enable that would go over is refused (`409`, `skills_index_budget_exceeded`). Every enabled description is part of the index the agent reads on each turn, so long descriptions use the 8000-character budget quickly: keep them to what the agent needs to decide when to load the skill.
   - **Precedence.** A skill is reference guidance with the lowest priority: Orbit's core rules win, then the company persona and rules, then the template guide, then skills. A skill cannot add tools, widen permissions or remove the user's confirmation of backend changes, and the agent is told so each time one is loaded.
   - **Changes apply on the next turn.** Skills are resolved at the start of every turn and are not pinned to a session (unlike the persona), so turning a skill off or deleting it takes effect for running sessions on their next message. The timeline shows which skills the agent used.
   - **Privacy.** The text of a skill is sent to the configured model provider whenever the agent loads it. Do not put secrets or personal data in a skill. Uploads that contain a recognizable credential (private key, `sk-` and similar API keys, AWS, GitHub, Slack or Google tokens, JWTs) are rejected; this check is a safety net, not a guarantee. Skill changes are audit-logged (`builder.skill.create`, `version`, `enable`, `disable`, `delete`) with the name, version, hash and size, never the text.
   - **API.** `GET`/`POST /dashboard/api/builder/skills` and `GET`/`PATCH`/`DELETE /dashboard/api/builder/skills/{name}`, superadmin only. Errors carry a machine `reason`; validation failures (`400`, `skill_invalid`) add `fields` with one code per field.

## Isolation model

What the shipped manifests enforce:

- The builder cannot reach Postgres, the Kubernetes API, cloud metadata or the internet. Its only reachable peer is the registry proxy (Compose: `internal: true` network; Kubernetes: NetworkPolicy).
- The builder holds no LLM key, GitHub key or Render key. Its environment contains only its own settings and `BUILDER_SHARED_SECRET`. Build steps run with an environment built from scratch from an allowlist (no inherited variables), so the shared secret does not reach code the agent can influence.
- Build commands are fixed argument lists run directly, never through a shell: `npm ci` (or `npm install` when the lockfile changed, with `--ignore-scripts`), `npx tsc -b`, `npm run lint --if-present`, and `npx vite build`. Each step has a timeout and the whole process group is killed on timeout.
- File paths are resolved and rejected if they leave the session directory or point into `.git`, `node_modules` or `dist`. Only text files are written, within per-file and per-session size limits.
- The builder runs as a non-root user with a read-only root filesystem and all capabilities dropped. In Kubernetes it also has no service-account token.
- Orbit-to-builder requests carry a signed token that expires after 60 seconds and is bound to the session, method and path. The proxy accepts only `GET` and `HEAD`.
- The registry proxy sends a private-registry token only to its own upstream, and refuses to send one to a plain-`http` upstream unless the host is loopback.

What it does **not** protect against:

- **One shared builder for all sessions.** All sessions run in the same container, under the same user, on the same volume and the same npm cache. A malicious dependency or a prompt-injected build script running for one user's session could read or modify another session's files. The design assumes one installation belongs to one company; it is **not** isolation between hostile users.
- **Code the agent writes still executes.** `npm run lint` and the Vite configuration are files the agent can edit, so they can run arbitrary commands as the builder user. The blast radius is the container: no secrets in the environment, writable `/work` and `/tmp` only, and the network limits above.
- **CPU and memory limits come from the container runtime, not from the builder.** The builder process does not set resource limits itself. If you run it outside a container, only the step timeout and the output cap apply.
- **Public packages are trusted by the proxy.** The proxy forwards whatever the npm registry serves. There is no package allowlist, and `--ignore-scripts` is the only guard against install scripts.
- **Shared-secret compromise.** Anyone holding `BUILDER_SHARED_SECRET` can call the builder and mint preview tokens. Protect it like any other credential and rotate it by updating Orbit and the builder together.
- **Network enforcement depends on the platform.** See [Deployment caveats](#deployment-caveats).
- **Scoped private registries are limited.** The proxy picks the upstream from the first path segment of the request. A scoped registry whose tarball URLs do not start with `/@scope/` (for example one served under a base path, such as `/reg/`) falls through to the default upstream and installs fail with `E404`. This was reproduced with Verdaccio; other registries served under a base path (Artifactory, Nexus) and GitHub Packages are expected to behave the same but were not tested. Only registries that serve tarballs at the root `/@scope/...` path are supported.

## Preview security

The preview serves static output generated from untrusted code, so it must not run with the dashboard's privileges. Three measures are applied together:

1. Every preview response carries `Content-Security-Policy: sandbox allow-scripts allow-forms allow-popups allow-modals`, which gives the page an opaque origin even if it is opened in its own tab.
2. The dashboard embeds it in an `<iframe sandbox="allow-scripts allow-forms allow-popups allow-modals">` **without** `allow-same-origin`.
3. The dashboard session cookie is `HttpOnly`, `SameSite=Strict` and `Path=/dashboard`, so the browser does not send it from the opaque origin.

Because the sandboxed frame has no cookie, the preview route authenticates with a short-lived signed token instead. The dashboard asks for one with `POST /dashboard/api/builder/sessions/{id}/preview-token`. The token is an HMAC (keyed with `BUILDER_SHARED_SECRET`), valid for 10 minutes and bound to the session id and the owner's user id. It travels in the URL **path**, not in a cookie or query string: the iframe URL is `/dashboard/api/builder/sessions/{id}/p/{token}/`. The preview is built with a relative Vite base (`--base ./`), so `./assets/...` resolves under the same `/p/{token}/` prefix and scripts and stylesheets are authorized too; the proxy sets no cookie at all. Because the token is in the path, Orbit redacts that segment (`/p/***/`) in request logs and sends no `Referer` (`Referrer-Policy: no-referrer`); keep it out of any other log you add in front of Orbit (reverse proxy, CDN) or the token is exposed for its 10 minutes of validity. A token for another session, another user, or an expired or forged token gets `401` and nothing is forwarded. The token, the dashboard cookie and any `Authorization` header are never forwarded to the builder. Only `GET` and `HEAD` are accepted, and the route replaces the dashboard's `X-Frame-Options: DENY` with `SAMEORIGIN` so the dashboard can frame it.

The previewed app talks to the backend app's REST API like any other frontend. The REST API's CORS policy allows any origin without credentials, so the opaque-origin page can call it with an `Authorization` header. Browser storage (`localStorage`) is not available in an opaque origin, so the default template falls back to memory for the Orbit client's token storage; login does not persist across reloads of the preview. This affects only the preview, not the published app.

The first design carried the token in a path-scoped cookie; it was replaced on 2026-10-27 because browsers do not send cookies from a sandboxed (opaque-origin) iframe reliably, so scripts and stylesheets came back `401` and the page stayed blank. Consequences for generated apps: the template router is a `HashRouter` (the app is served under a changing path), and absolute `/something` URLs in code do not resolve in the preview (import assets from `src/` or use relative paths). The published app is built with Vite's default base `/` and is not affected. Verify the preview in a real browser on your own deployment.

## Publishing

Publishing reuses the existing frontend-app pipeline.

- **First publish.** The user optionally enters a subdomain. Orbit checks that GitHub and the deploy provider are configured, creates a private repository, pushes **one commit** containing the session's current tree, creates the frontend app record linked to the session's backend app, sets up the deploy key (as for any frontend app), and creates the Render static site. The default template is used for the build command and publish path.
- **Publishing again.** Orbit pushes one more commit on top of the last published one, and Render's auto-deploy builds it. Turns between publishes are **not** pushed.
- **History.** GitHub receives one commit per publish, not one per turn. Per-turn history stays in the session.
- **Failures.** Preconditions are checked before any side effect and answer `409` with a `reason` (`github_not_configured`, `deploy_not_configured`, `template_not_deployable`, `template_unavailable`, `nothing_to_publish`, `turn_in_progress`, `publish_in_progress`, `remote_moved`). If the branch was changed outside the builder (`remote_moved`), nothing is pushed and the remote is never overwritten. A Render failure after the code is on GitHub leaves the frontend app in place with a failed deploy status, and the existing deploy retry applies. The session is never modified by a failed publish.
- **Without GitHub or Render.** The user can still download the source as a ZIP (`GET /dashboard/api/builder/sessions/{id}/download.zip`).

## Sizing

:::caution
These numbers come from one local run with **scripted builds on the default template**, not from real LLM sessions. Real sessions install more dependencies and run more builds. Re-measure on your own hardware and model before you rely on them.
:::

Measured on a local Docker run with the container limited to 2 CPUs and 2 GiB, default template:

| Measurement | Result |
|-------------|--------|
| One build, peak container memory | about 487 MiB |
| Three builds at the same time, peak container memory | about 1177 MiB |
| `node_modules` per session | about 110 MB |
| Shared npm cache | about 23 MB |

The memory figures are the cgroup `memory.peak`, which includes page cache, so they overstate the memory the processes actually hold.

What this means for the defaults:

- The builder allows `BUILDER_MAX_ACTIVE_SESSIONS` sessions at once (default `4`, range 1 to 1024). Beyond that it answers `429`, and Orbit keeps the turn `queued` and shows a waiting state. The shipped manifests do not set this variable, so the default of 4 applies.
- Each Orbit replica runs `BUILDER_WORKER_CONCURRENCY` turns (default `2`), so **2 Orbit replicas can send up to 4 concurrent builds**. For that reason the Helm chart and the Kustomize Component set the builder memory limit to `4Gi` (request `512Mi`, CPU limit `2`).
- The Compose file uses `mem_limit: 4g` and `cpus: 2`, the same memory as the Helm chart. It was sized from the measurement above, not from real LLM sessions; adjust it, or lower `BUILDER_MAX_ACTIVE_SESSIONS`, based on your own measurements.
- Plan disk for the workspace volume as roughly 110 MB of `node_modules` per session, plus sources and the shared npm cache. The Helm default volume is `10Gi`.
- Session workspaces are not purged automatically (see [Known limitations](#known-limitations)).

Nothing here has been measured with real LLM sessions, with several users, or with a larger template.

## Deployment caveats

- **Easypanel and Docker Swarm: isolation is unverified.** Whether Easypanel honors `internal: true` networks and profiles when it runs a Compose app has not been tested, and `easypanel.json` does not define the builder services. Do not assume the builder is isolated from the database there. If you deploy it on Easypanel or Swarm, verify from inside the builder container that it cannot reach Postgres or the internet before enabling the feature.
- **Kubernetes NetworkPolicy needs a CNI that enforces it** (for example Calico or Cilium). On a cluster whose CNI ignores NetworkPolicy, the policies in the chart and the Component are accepted and do nothing, and the builder is not isolated.
- **The builder is a single replica.** Its workspaces live on a `ReadWriteOnce` volume and the Deployment uses the `Recreate` strategy, so an upgrade or a node loss interrupts running builds briefly. Orbit restores sessions from Postgres snapshots when the volume is lost.
- **Compose host access.** `deploy/builder.compose.yml` is a development and evaluation stack with an `edge` TCP forwarder so a host process can reach the builder; it is not a production file. The `docker-compose.yml` profile is the reference topology.

## Environment variables

Orbit:

| Variable | Default | Where it is read |
|----------|---------|------------------|
| `BUILDER_URL` | empty (feature off) | `internal/dashboard/builder_runtime.go` |
| `BUILDER_SHARED_SECRET` | none, at least 16 bytes | `internal/dashboard/builder_runtime.go` |
| `BUILDER_WORKER_CONCURRENCY` | `2` | `internal/agent/worker.go` |

Builder daemon (`zeep-builder`):

| Variable | Default | Description |
|----------|---------|-------------|
| `BUILDER_SHARED_SECRET` | required, at least 16 bytes | Same value as on Orbit. The daemon exits at start-up if it is missing or short. |
| `BUILDER_ADDR` | `:8081` | Listen address. |
| `BUILDER_WORK_DIR` | `/work` | Directory holding one subdirectory per session. |
| `BUILDER_TEMPLATES_DIR` | `/opt/templates` | Directory holding the built-in templates (`default`). |
| `BUILDER_MAX_ACTIVE_SESSIONS` | `4` | Concurrent active sessions, 1 to 1024. |
| `BUILDER_NPM_REGISTRY` | unset | Registry URL handed to npm; set it to the registry proxy (for example `http://registry-proxy:4873`). Must be an `http` or `https` URL. |
| `BUILDER_NPM_CACHE` | `<BUILDER_WORK_DIR>/.npm-cache` | Shared npm cache, used by all sessions. |
| `BUILDER_HOME` | `/tmp` | `HOME` of build steps. Must be an absolute path. |

Registry proxy (`zeep-registry-proxy`):

| Variable | Default | Description |
|----------|---------|-------------|
| `REGISTRY_PROXY_ADDR` | `:4873` | Listen address. |
| `REGISTRY_PROXY_UPSTREAMS` | `https://registry.npmjs.org` | Comma-separated upstreams. A bare URL is the default upstream; `@scope=URL` is an upstream for that scope. If entries are given but none is bare, unscoped packages are rejected with `403`. |
| `REGISTRY_PROXY_TOKEN_<NAME>` | unset | Bearer token for one upstream, sent only to that upstream. `<NAME>` is `DEFAULT` for the bare upstream; for a scope it is the scope uppercased, with the leading `@` dropped and every non-alphanumeric character replaced by `_` (`@my-org` becomes `MY_ORG`). Refused for a plain-`http` upstream unless the host is loopback. The Compose file, the chart and the Component pass `REGISTRY_PROXY_TOKEN_DEFAULT` and `REGISTRY_PROXY_TOKEN_COMPANY` only. |

## Known limitations

- **No hot reload.** The preview serves the output of the last successful build and reloads when a build succeeds. There is no Vite HMR.
- **Deep links in the published app return 404 on Render.** The published frontend is a Render static site, and no SPA rewrite rule is configured, so opening a client-side route directly (not through the home page) returns `404`.
- **No cleanup of orphaned sessions.** Workspaces of sessions that are never deleted stay on the builder volume, and archived sessions keep their snapshots in Postgres. There is no retention job.
- **No per-user session cap.** A user can create sessions without a limit. Only the token budgets and the builder capacity bound usage.
- **The configuration routes are not gated by `BUILDER_URL`.** `GET`/`PUT /dashboard/api/builder/settings`, `/prompt` and `/prompt/versions`, `GET /dashboard/api/builder/templates` and `PATCH /dashboard/api/github/templates/{id}/builder` work before a builder is deployed, so a superadmin can prepare the configuration first. Session routes answer `503` until `BUILDER_URL` is set.
- **Deleting a session that has an active turn archives it.** `DELETE /dashboard/api/builder/sessions/{id}` archives the session and destroys its workspace without waiting for the running turn. Published frontend apps are not touched.
- **One turn at a time per session.** Sending a message while a turn is running is rejected.
- **One builder replica.** Multiple builder instances and per-session containers are not supported.
- **Provider support.** Only `openai` and `openai_compatible` are supported. Native Anthropic and Gemini adapters are not implemented. Gemini's OpenAI-compatible endpoint is documented by Google as beta; whether it handles tool calls correctly with this builder has not been verified here.
- **Publishing needs GitHub and Render.** Other publish targets are not supported.

## Troubleshooting

`GET /dashboard/api/builder/status` returns `{"available": false, "provider_configured": ..., "reason": "..."}`. The dashboard shows a message based on the `reason`. The checks run in this order, and the first failing one is reported.

| `reason` | Meaning | What to check |
|----------|---------|---------------|
| `not_configured` | Orbit has no builder wired: `BUILDER_URL` is empty. | Set `BUILDER_URL` and `BUILDER_SHARED_SECRET` on every Orbit replica and restart. In Compose, start with `--profile builder`. In Helm, set `builder.enabled=true`. With Kustomize, check that the Component is included in the overlay. |
| `disabled` | The builder is wired, but **Enable AI Builder** is off in the settings. | A superadmin turns it on in **Integrations**, **AI Builder** tab. |
| `provider_not_configured` | The model provider chosen in the builder settings is unusable. | In the **AI** tab, check the provider row: it must be enabled and have a model and, for `openai`, an API key; for `openai_compatible` it needs a base URL. |
| `budget_not_configured` | A token cap is `0` (unset), so every turn would end with `budget_exceeded`. | In the **AI Builder** tab, set both the per-session and the per-user monthly token caps to a value greater than zero. Until then the builder is unavailable and **Create with AI** is disabled. |
| `builder_unreachable` | Orbit could not get a healthy answer from the builder. | Check that the builder container or pod is running, that `BUILDER_URL` resolves from Orbit, and that the network (Compose network, NetworkPolicy ingress from the Orbit pods on port 8081) lets Orbit through. A different `BUILDER_SHARED_SECRET` on the two sides makes every request fail with `401`, which also shows up as unreachable. The builder health endpoint requires a signed token, so a plain `curl` to it answers `401`; use the builder's logs instead. |

Other symptoms:

- **A turn ends right away with `budget_exceeded`.** A budget was reached or was set to `0` while the turn was running. Check the session budget and the monthly per-user budget; both must be greater than zero.
- **A turn stays `queued`.** The builder is at capacity (`BUILDER_MAX_ACTIVE_SESSIONS`), or Orbit replicas are running their maximum number of turns (`BUILDER_WORKER_CONCURRENCY`). It starts when a slot frees up.
- **A turn ends with a generic error.** Users see only a generic message by design; provider and builder error text is logged on the Orbit server, not returned. Look in Orbit's server logs.
- **`npm install` fails with `E404` for a private package.** See the scoped-registry limitation under [Isolation model](#isolation-model), and check `REGISTRY_PROXY_UPSTREAMS` and the matching `REGISTRY_PROXY_TOKEN_<NAME>`.
- **Preview shows `401`.** The preview token expired (10 minutes) or belongs to another user or session. Reload the session page.
