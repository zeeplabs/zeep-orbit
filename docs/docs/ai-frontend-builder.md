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
- **Everything the agent can do is a fixed tool.** `list_dir`, `read_file`, `write_file`, `edit_file`, `run_build` (`install`, `typecheck`, `lint`, `build` only), `get_build_logs`, `orbit_get_app_schema`, `orbit_list_auth_providers` and `propose_backend_change`, plus `ask_user` and `propose_build_plan` while [planning](#planning) is on. There is no shell tool.
- **Backend changes need confirmation.** The two Orbit read tools are scoped to the session's linked backend app and run with the session owner's permissions. When the agent wants a new table, column or policy, it emits a proposal and the turn pauses. The user confirms or rejects it; a confirmed proposal is applied through the same path as "Edit with AI" (same validation and RBAC, audit origin `ai_builder`) and requires write access on the backend app.
- **One commit per turn.** After each turn the builder commits the changes, and Orbit stores an incremental `git bundle` of that commit in Postgres (`builder_snapshots`). The builder volume is a cache: if it is lost, Orbit restores the session from those snapshots.
- **Reverting** restores the working copy to the state after an earlier turn as a *new* commit (history is not rewritten), rebuilds the preview, and makes no model call, so it spends no tokens.
- **Sessions are owner-only.** A superadmin cannot read other users' sessions. Using the builder requires at least viewer access on the backend app; confirming a backend change requires editor or admin.

## Home chat and My projects

When the builder is available, the dashboard opens on a chat-style home instead of the apps list. This is a second way in; the existing screens and deep links are unchanged.

- **Home (`/dashboard/`).** One message box. Starting a project creates a builder session and sends the message as its first turn, then opens `/builder/:id`. The title is the first 60 characters of the message, cut at a word boundary. A selector chooses the backend: a new one (created by the builder) or one of the backend apps the user can already see. Four suggestions fill the box without sending. "Continue where you left off" lists the three most recently updated sessions.
- **If the first message fails** (for example a rate limit), the session that was just created is kept and the next attempt sends the message to it, so no empty session is left behind and nothing is deleted. A backend that the builder created is not removed when a session is archived; delete it from **Apps** if it is no longer needed.
- **My projects (`/dashboard/projetos`).** One card per builder session the user owns (title, status, backend app, last update, "Open chat" and "Backend") and one card per backend app the user can see that none of the user's sessions uses ("Open backend" and "Create frontend with AI", which starts a session on that backend). Status is *Published* when the session has a published frontend, *Building* with the current milestone (for example "milestone 2 of 4") when it has a brief, and a neutral label otherwise; a backend without a session is *Backend only*. Filters (All, Building, Published, Backend only) and the search box work on the loaded list. A backend used only by another user's session also appears as *Backend only*, because sessions are owner-only.
- **The builder page** opens in the same shell, with the sidebar collapsed to icons (each with a name and a tooltip). `/builder/usage` and every other deep link (`/apps/:id`, `/data-browser`, `/logs` and so on) keep the advanced layout.
- **Advanced mode.** A switch in the header (simple shell) and at the bottom of the advanced sidebar toggles between the two interfaces. The choice is stored in the browser (`localStorage`, key `orbit.mode`, `simple` or `advanced`); it is per browser, not per account, and falls back to memory when storage is blocked. With the builder available the default is simple. In advanced mode `/dashboard/` opens `/apps`.
- **Behavior change:** after login, and on `/dashboard/` for a signed-in user, Orbit now goes to `/dashboard/` (it used to go to `/dashboard/apps`). Users whose saved mode is advanced, and every user on an installation where the builder is not available (`BUILDER_URL` unset, disabled, or any other reason in `GET /dashboard/api/builder/status`), still end up on `/apps`; the home and My projects are never shown without the builder. A superadmin sees the reason on **Apps**, in the disabled "Create with AI" entry.
- **API.** Each row of `GET /dashboard/api/builder/sessions` carries `brief_version`, `milestones_total` and `milestones_done` (zeros when the session has no brief), computed for the whole list in two queries.
- **On a phone** the sidebar is replaced by a bottom bar (Chat, My projects, More); More opens a sheet with New project, recent projects and the account actions.

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
- `zeep` is attached to `default` and `builder_edge`, **not** to `builder_internal`, so code running in the builder cannot open a connection to `zeep:8080`. It reaches the builder through `builder-edge`, a TCP forwarder (`alpine/socat`, no application logic) on `builder_edge` and `builder_internal`, both `internal: true`. On `builder_edge` the forwarder answers as `builder`, so `BUILDER_URL=http://builder:8081` is unchanged. The builder never initiates a connection to Orbit; Orbit always calls the builder.
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
| `builder.registryProxy.tokenDefault`, `tokenCompany` | empty | Stored in a Secret of their own (`<fullname>-registry-proxy`, rendered only when a token is set) and exposed only to the proxy as `REGISTRY_PROXY_TOKEN_DEFAULT` and `REGISTRY_PROXY_TOKEN_COMPANY`. They are not in the main chart Secret, which the Orbit pods load whole with `envFrom`. |

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

1. **Model provider** (the **AI** tab). The builder uses a provider row of the existing AI provider configuration. Three kinds are supported:
   - `openai`: the OpenAI API, with an API key and a model.
   - `openai_compatible`: any server that speaks the OpenAI Chat Completions API (for example Ollama, vLLM, Azure OpenAI or a gateway). It needs a **base URL** and a model. The API key is optional for this kind.
   - `anthropic`: the Anthropic Messages API, with an API key and a model. It has no base URL setting (a gateway is reached through `openai_compatible`).

   For `openai` and `anthropic` the model is picked from Orbit's model catalog (see [Model prices](#model-prices)); the API refuses any other model with `model is not in the catalog`. A provider row left on a model outside the catalog (for example one saved before the catalog existed) keeps the builder unavailable with the reason `model_not_in_catalog` until a superadmin picks a catalog model. The "Build with AI" and "Edit with AI" drawers keep working with it.

   The key is encrypted at rest and never returned by the API (only `has_key`). Updating the model without sending the key keeps the stored key.

   The base URL must be an absolute `http` or `https` URL without credentials, query string or fragment. Private and loopback hosts are allowed (an Ollama container on an internal network is a supported setup); link-local and cloud-metadata addresses are rejected. The check is made when you save and again when the connection is made, on the IP address actually dialed, so a hostname that resolves to a blocked address is also refused. If `HTTP_PROXY` or `HTTPS_PROXY` is set in Orbit's environment, the connection goes to that proxy, so the dial-time check sees the proxy's address. Orbit therefore also checks the target host (literal and resolved addresses) before it chooses the proxy; what the proxy itself resolves is covered by the proxy's own egress policy.

2. **Builder settings** (the **AI Builder** tab).

   | Setting | Default | Allowed range |
   |---------|---------|---------------|
   | Enabled | off | |
   | Model provider | `openai` | `openai`, `openai_compatible` or `anthropic` |
   | Context window (tokens) | `128000` | 4096 to 2000000 |
   | Session token budget | `0` (unset) | 0 to 10^12 |
   | Monthly token budget per user | `0` (unset) | 0 to 10^12 |
   | Currency | `USD` | a three-letter ISO 4217 code |
   | Session cost budget | `0` (no money limit) | an amount in USD |
   | Monthly cost budget per user | `0` (no money limit) | an amount in USD |
   | Max steps per turn | `40` | 1 to 200 |
   | Max fix iterations (failed builds per turn) | `3` | 1 to 20 |
   | Max seconds per turn | `600` | 10 to 3600 |
   | Planning | `required` | `required`, `suggested` or `off` (see [Planning](#planning)) |
   | Planning model | empty (the provider's own model) | a model of the selected provider (see [Planning model](#planning-model)) |

   **Max steps per turn** counts the model calls that do work. A turn stopped by it, or by **Max fix iterations**, ends `limit_reached` after one extra closing call, outside the limit and with tools disabled, that asks the model for a handover: what is done, what is missing and what to ask next (for the fix limit, which build error persists and what to check). A turn that finishes within the limit has no closing call, and a closing call that fails or does not fit the budget or the turn time only means there is no handover. The closing call is billed like any other.

   Token budgets count every token of a call: input, cached input, cache write and output. **Both token budgets must be greater than zero.** With either one at `0`, the builder reports itself unavailable (`budget_not_configured`) and **Create with AI** is disabled; sending a message to an existing session is refused the same way. If a cap is set back to `0` while a turn is already running, that turn's next model call is refused and the turn ends with a `budget_exceeded` event. The budget is checked before each model call, against what was already recorded, so the call that crosses the limit is the last one. The monthly budget counts the calendar month in UTC. A settings change applies to the next turn without restarting Orbit.

   The two cost budgets are optional and work the same way, in money: see [Usage and cost](#usage-and-cost). When usage reaches 80% of any budget, the session timeline shows a notice, once per session and budget.

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

## Planning

Before it writes code, the agent can ask the user a few questions and propose a build plan. The approved plan becomes the session's **brief**: the agent reads it on every later turn, and the app is built one milestone at a time. The goal is fewer wrong guesses on the first build, which are the most expensive turns.

### The setting

A superadmin sets **Planning** in **Integrations**, **AI Builder** (`planning_mode` in `PUT /dashboard/api/builder/settings`).

| Value | New session | Later in the session | "Plan first" switch |
|-------|-------------|----------------------|---------------------|
| `required` (default) | Starts with questions and a plan to approve. No code is written before the plan is approved. | The agent may ask or propose a plan before a large or unclear change. | Shown |
| `suggested` | Starts building as before. The agent plans first when the request is new, large or unclear. | Same as `required`. | Shown |
| `off` | Starts building as before. | The planning tools are not offered. | Hidden |

:::note Behavior change on upgrade
`required` is also the default for installations that already used the builder: after the upgrade, **new** sessions start with the questions. Sessions that already exist keep building as before, and have no brief until a plan is approved in them. Set **Planning** to `suggested` or `off` to keep the previous behavior.
:::

Switching from `required` to `off` while a session is still in its questions moves that session to building on its next message. The answers stay in the conversation, which the agent reads, but no brief is created from them. A turn already paused on a question or a plan finishes as it started.

### Planning model

Planning is a share of the spend that does not write code: the agent asks questions and writes a plan. A superadmin can run those turns on a cheaper model with **Planning model** in **Integrations**, **AI Builder** (`planning_model` in `PUT /dashboard/api/builder/settings`).

- **The default changes nothing.** Left empty, every call of every turn uses the provider's own model, as before.
- **Same provider only.** The planning model is a model of the provider selected in **Model provider**: one key, one set of failure modes. For `openai` and `anthropic` it must be a catalog model (the API refuses any other with `planning_model must be a model of the selected provider's catalog`); a deprecated model the setting already holds stays valid. For `openai_compatible` it is any short text. Changing the provider clears the saved planning model unless the same request sets a new one.
- **What runs on it.** Calls of a plan-mode turn: the questions, the plan, and a revision of a rejected plan. When the user approves a plan and the same turn goes on to build, the build calls use the provider's own model. History compaction always uses the provider's own model. Each call is recorded and priced under the model that answered it.
- **It never stops the builder.** If the planning model cannot be used (for example it left the catalog, or the provider cannot be built), the planning turn runs on the build model and a warning is logged. Only the build model decides whether the builder is available.
- **Budgets.** Token and cost budgets count the calls of both models together.
- **Check the share.** In the usage report, group by **Purpose** (`group_by=purpose`) to see what planning costs against building, and by **Model** to see which model answered.
- **Decide with evidence.** Whether a cheaper model plans well enough is a quality question. A test that is off by default and spends real tokens (`go test -tags planningeval`, see the header of `internal/agent/planning_eval_test.go`) sends the same 12 briefs through the real plan-mode turn on each model and prints one result line per brief, with the plan text anonymized for a blind read.

### Questions

While planning, a turn runs in **plan mode**: the agent is offered only the read tools (`list_dir`, `read_file`, `get_build_logs`, `orbit_get_app_schema`, `orbit_list_auth_providers`, and `load_skill` when skills are enabled) and the two planning tools. It cannot change files, run a build or propose a backend change. This is enforced by the agent loop, which refuses any other tool call without running it, not only by the prompt. With an existing backend app, the agent is told to read its schema before asking.

`ask_user` shows a card in the chat with 1 to 4 questions, each with 2 to 5 options (single or multiple choice). The card always adds a text field and **Decide for me**; a question left blank is also decided by the agent. **Send answers** sends the round; **Start building now** sends what was picked and ends the questions, and the agent then proposes the plan with what it has, listing the open points as assumptions.

The agent may ask at most 3 rounds in a plan-mode turn and 1 round in a build turn. Over that, the call is refused and the agent is told to propose the plan. "Back to questions" on the plan card allows one more round.

### Build plan

`propose_build_plan` shows the plan as a card: a summary, the screens, the data (each table with who can read it), the look, the agent's **assumptions** (set apart, because the user did not decide them), what is not included, 1 to 8 numbered milestones and an estimate of the cost (below). A table readable by anyone without signing in, or with no declared access, is highlighted. The data list is intent in words: tables are created later through a backend proposal, which the user confirms as usual.

- **Build this** approves the plan. It is stored as the next brief version and the same turn goes on to build the first milestone that is not done, with no other message.
- **Ask for changes** sends the user's words (up to 2000 characters) back to the agent, which proposes a new plan.
- **Back to questions** asks for one more round of questions, then a new plan.

A plan's arguments must fit in 8 KiB and its compact form in 6000 characters; over either limit the call is refused and the agent shortens the plan. The brief in the prompt is therefore never cut.

### Brief and milestones

- **Versions.** Each approval stores a new version of the brief (`builder_briefs`). The newest one is the brief. A session with a brief has a **Brief** button in its header, with the milestones built (for example "Brief · 1/3"), that opens the brief, the state of each milestone and the older versions, read-only. To change the brief, ask the agent in the chat: it proposes a revised plan, which the user approves as a new version.
- **In the prompt.** The brief is added to the agent's prompt as `APP BRIEF`, after the company rules, the template guide and the skills index, with lower priority than Orbit's core rules and the company standards. The current milestone and the progress go in the session context.
- **In the code.** Build turns write the brief to `ORBIT_BRIEF.md` at the root of the project, next to `ORBIT_AGENT.md`, when the file differs from the brief. It ships in the downloaded ZIP and is not part of the built site.
- **One milestone per turn.** The agent is told to build one milestone, stop and say what is next. After a turn, **Continue: \<next milestone\>** builds the next one. A milestone is done when a turn that built it ends `completed`, or when a revised plan marks it as already built. A turn that fails or hits a limit leaves it pending, and **Continue** offers it again. A turn the agent ends early still counts as done; the user corrects it in words.
- **Plan first.** In a session that is already building, the **Plan first** switch next to the message box sends the next message as a plan-mode turn: questions and a plan, and no code written until it is approved.

### Estimate

The plan card shows a range of tokens and, when the data allows it, of cost, for one milestone and for the milestones of the plan not yet built. It is computed by the server from the completed build turns of the installation in the last 90 days, for the provider and model configured now: the 25th to 75th percentile per turn. Under 10 such turns the card says there is no estimate yet; under 10 turns with a price for every call, it shows tokens only. Cost uses only turns after the last currency change.

- The figures come from every user of the installation, aggregated. A user can therefore infer the typical cost of a build on the installation.
- The first build of an app usually costs more than a later milestone. When there are at least 10 completed turns of milestone 1 and at least 10 of later milestones, the plan range is the first-milestone range plus the later range for each remaining milestone (`basis: "split"`). With fewer in either group, one range is used for every milestone (`basis: "single"`). Turns that planned are not part of either sample.
- It is an estimate from past builds, not a quote and not a budget.

### Data and audit

Answers and plans are chat content: they are sent to the configured model provider and stored with the conversation, like messages. They are not written to the audit log. Answering a round, approving and revising a plan are audited without their text (`builder.questions.answer`, `builder.plan.approve` with the version and the number of milestones, `builder.plan.revise`). Model calls of plan-mode turns are recorded with the purpose `planning` and count toward the budgets like any other call.

### API

All routes are owner-only, under `/dashboard/api/builder/sessions/{id}`. The three `POST` routes are rate-limited like `confirm` and require the `tool_call_id` of the card the user acted on: a card that was replaced answers `409`, and a repeated request answers `200` with `already_resolved`.

- `POST .../turns/{turn}/answers` with `{tool_call_id, answers: [{id, choices, text, decide}], skip}`.
- `POST .../turns/{turn}/plan/approve` with `{tool_call_id}`.
- `POST .../turns/{turn}/plan/revise` with `{tool_call_id, feedback, more_questions}`; `feedback` is required unless `more_questions`.
- `GET .../turns/{turn}/plan/estimate`: `{available, sample_turns, currency, milestones, milestone, plan, basis}` (`basis` is `split` or `single`, absent when not available), each range with `tokens_low`, `tokens_high`, `cost_micros_low` and `cost_micros_high` (`null` without a price).
- `GET .../brief` (`version: 0` when there is none) and `GET .../brief/versions/{v}`.
- `POST .../messages` accepts `plan_first` (refused when planning is `off`) and `milestone` (only the next pending milestone of the brief, otherwise `400`). The session detail carries `phase`, `brief_version`, `milestones_total`, `milestones_done` and `next_milestone`; each turn carries `mode`, `brief_version` and `milestone`.

## Usage and cost

Orbit records every model call the builder makes: the provider and model, what the call was for (a chat step or a history compaction), and its tokens in four classes (input at the full rate, cached input, cache write, output). The record holds counts and identifiers only, never prompt or reply text.

### Model prices

Prices are **per million tokens**, in USD, for input, cached input, cache write and output. They are shown under **Integrations**, **AI Builder**, in the **Model prices** card.

- **OpenAI and Anthropic.** The models and their list prices come from a catalog that ships with Orbit and changes only with an Orbit release; Orbit never fetches prices. The card shows them read-only with the date they were checked against the providers' pricing pages, and the price API refuses to write them. A model can have prices by prompt size: Claude Haiku 5.5 costs five times more when the prompt (input, cached input and cache write of the call) is over 100,000 tokens, and each call is billed at the tier its prompt falls in. A release can also carry a price that starts on a later date; the card shows it with that date.
- **OpenAI-compatible servers.** Their models have no list price, so a superadmin enters one row per model in the same card. When an optional price (cached input, cache write) is empty, those tokens are charged at the input price.

The card also lists the models used in the last 30 days that have no price: OpenAI-compatible models with no row, and OpenAI or Anthropic models that are not in the catalog.

The cost of a call is computed once, when the call is recorded, and stored with it. A price change (a new release, or an edited OpenAI-compatible row) affects only new calls; it does not rewrite history.

**"No price set"** means the model of a call had no price when the call was made. Such a call has no cost: it is not counted as zero, it does not stop the call, and it does not count toward the cost budgets. A total that mixes priced and unpriced calls is shown as the known amount plus the number of calls with no price, never as a full amount.

The figures are estimates from list prices and your own rows. They are not an invoice: discounts, taxes and the provider's own rounding are not included, so compare them with the provider's billing before relying on them.

### Currency

All prices, costs and money budgets are in USD, the currency both providers publish in; it cannot be changed. An installation that used another currency is moved to USD once, when Orbit starts on this version: the cost budget numbers are kept as they were, and the **Costs** card asks the superadmin to review them until the card is saved. Costs already recorded are not converted, and the usage report shows a note when the currency changed inside or after the selected period.

### Cost budgets

The **Costs** card has two optional budgets in money, next to the token budgets: one per session and one per user per calendar month (UTC). `0` means no money limit. They are checked before each model call against the cost already recorded, so the call that crosses the limit is the last one. Calls with no price do not count, so a cost budget does not limit a model that has no price (an OpenAI-compatible model with no row); the card warns when a cost budget is set and the model in use has no price. The token budgets always apply.

When a turn stops because of a budget, the chat says which one (session or month, tokens or cost).

### Where it is shown

- **In the chat.** Each finished step shows its tokens and cost in one line. Opening it shows the receipt: model calls, tokens by class, cost, working time, files changed, builds and the model.
- **In the session.** A strip under the page header shows the session total and, for each budget that is set, how much of it is used.
- **In the usage report** (`/dashboard/builder/usage`, linked from the apps page, from a session and from the **Costs** card). It shows a period of up to 366 days, grouped by day, model, purpose (chat, planning or compaction) or session, with the totals, the share of tokens spent on history compaction and a CSV download with one line per model call. Days are counted in UTC. A superadmin sees every user's usage, can group by user and can select a user to see only theirs. Everyone else sees only their own usage.

Usage per user is information about a person's activity. Only superadmins can read other users' usage, and a session's title is shown in the report only to the session's owner.

### API

- `GET /dashboard/api/builder/catalog` (any signed-in user): the model catalog: `currency`, `reviewed_at` and, per provider, the models with `deprecated` and their `prices`, each with `effective_from` and `tiers` (`max_prompt_tokens`, `0` meaning no limit, and the four prices as decimal strings, `null` when billed at the input price).
- `GET`/`PUT`/`DELETE /dashboard/api/builder/prices` (superadmin): the price table of OpenAI-compatible models. A price row has `provider`, `model`, `input_per_mtok`, `output_per_mtok` and the optional `cached_input_per_mtok` and `cache_write_per_mtok`, as decimal strings. A write or delete for `openai` or `anthropic` is refused with `prices of catalog providers cannot be changed`.
- `GET /dashboard/api/builder/sessions/{id}/usage` (session owner): totals, budgets in use and one receipt per turn.
- `GET /dashboard/api/builder/usage?from&to&group_by=day|user|model|purpose|session` and `GET /dashboard/api/builder/usage.csv?from&to`: the report and its export. `from` and `to` are UTC days (`YYYY-MM-DD`), both inclusive; the default is the last 30 days. A superadmin may add `user_id`; `group_by=user` is superadmin-only. The export is refused above 100000 rows.

Money is carried as integer micros (1 USD = 1,000,000 micros) in `cost_micros`; `null` means no price.

## Isolation model

What the shipped manifests enforce:

- The builder cannot reach Postgres, the Kubernetes API, cloud metadata or the internet. Its only reachable peer is the registry proxy (Compose: `internal: true` network; Kubernetes: NetworkPolicy).
- The builder holds no LLM key, GitHub key or Render key. Its environment contains only its own settings and `BUILDER_SHARED_SECRET`. Build steps run with an environment built from scratch from an allowlist (no inherited variables), so the shared secret does not reach code the agent can influence.
- Build commands are fixed argument lists run directly, never through a shell: `npm ci` (or `npm install` when the lockfile changed, with `--ignore-scripts`), `npx tsc -b`, `npm run lint --if-present`, and `npx vite build`. Each step has a timeout and the whole process group is killed on timeout.
- File paths are resolved and rejected if they leave the session directory or point into `.git`, `node_modules` or `dist`. Only text files are written, within per-file and per-session size limits.
- The builder runs as a non-root user with a read-only root filesystem and all capabilities dropped. In Kubernetes it also has no service-account token.
- Orbit-to-builder requests carry a signed token that expires after 60 seconds and is bound to the session, method and path. The proxy accepts only `GET` and `HEAD`.
- The registry proxy sends a private-registry token only to its own upstream, and refuses to send one to a plain-`http` upstream unless the host is loopback.
- The registry proxy forwards only the two read endpoints npm needs to install: package metadata (`/<name>`, `/@scope/<name>`, `/@scope%2F<name>`) and tarballs (`/<name>/-/<file>.tgz`, `/@scope/<name>/-/<file>.tgz`). Everything else, including the registry's own `/-/...` namespace (`/-/whoami`, `/-/npm/v1/tokens`, search, audit, dist-tags), gets `403` without reaching the upstream, so build code cannot use the private-registry token for account or token endpoints.

What it does **not** protect against:

- **One shared builder for all sessions: treat the builder as single-tenant.** All sessions run in the same container, under the same user (uid 1000), on the same volume and the same npm cache. Build steps only change their working directory to the session root; there is no per-session uid, chroot or namespace. A malicious dependency or a prompt-injected build script (the Vite config, an npm script) running for one user's session can read or modify another session's files under `/work`, including unpublished code, and can poison the shared npm cache (`/work/.npm-cache`) for later installs. The design assumes one installation belongs to one company whose users trust each other; it is **not** isolation between hostile users or between customers. Run one builder per tenant if that matters to you. The daemon marks itself non-dumpable on Linux, so a build step cannot read `BUILDER_SHARED_SECRET` from `/proc/<daemon pid>/environ` or attach to the daemon; that protects the secret, not the other sessions' files.
- **Code the agent writes still executes.** `npm run lint` and the Vite configuration are files the agent can edit, so they can run arbitrary commands as the builder user. The blast radius is the container: no secrets in the environment, writable `/work` and `/tmp` only, and the network limits above.
- **Disk limits are soft.** All sessions share one volume (`/work`). The builder caps each session root at 1 GiB on disk (`node_modules`, `dist`, `.git` and `.orbit` included): the size is measured every 5 seconds while a build step runs, the step's process group is killed when it passes the cap, and a session over the cap after a step fails that step and loses `node_modules/` and `dist/` (the next `install` and `build` recreate them). The shared npm cache (`/work/.npm-cache`) is emptied when it passes 2 GiB and no step is running. These caps are constants in the builder, not settings. They are not a quota: a step can overshoot by what it writes in 5 seconds, and code running in a step can write outside its session (anywhere in `/work`, in `/tmp`) because every session runs as the same user. Size the volume with headroom, and watch its usage.
- **CPU and memory limits come from the container runtime, not from the builder.** The builder process does not set resource limits itself. If you run it outside a container, only the step timeout and the output cap apply.
- **Public packages are trusted by the proxy.** The proxy forwards whatever the npm registry serves. There is no package allowlist, and `--ignore-scripts` is the only guard against install scripts.
- **Shared-secret compromise.** Anyone holding `BUILDER_SHARED_SECRET` can call the builder and mint preview tokens. Protect it like any other credential and rotate it by updating Orbit and the builder together.
- **Network enforcement depends on the platform.** See [Deployment caveats](#deployment-caveats).
- **DNS is an exfiltration channel in Kubernetes.** The builder's NetworkPolicy allows port 53 to `kube-dns`, because the builder reaches the registry proxy by its Service name. The cluster resolver forwards names it does not own to upstream resolvers, so code in a build step can leak data by looking up names under a domain its author controls (for example `<data>.attacker.example`), even though it cannot open a connection to the internet. The data reachable that way is what the build step can read: the session files under `/work`, other sessions included (see above). Removing the DNS rule would require the builder to reach the proxy by IP (a fixed `clusterIP` on the proxy Service, or `hostAliases` with that IP), which depends on each cluster's Service CIDR and has not been tested, so the chart does not do it. Options if this matters to you: a DNS-aware policy that only resolves cluster names (Cilium `toFQDNs`, or a CoreDNS view or policy plugin for the builder's namespace), or a fixed `clusterIP` plus `hostAliases` in your own overlay and no port-53 egress. In Docker Compose the builder sits on an `internal: true` network; whether Docker's embedded resolver forwards external names for containers on internal-only networks depends on the Docker version and was not checked here.
- **A private scope that is not configured goes to the public registry.** A package scope with no `@scope=URL` entry in `REGISTRY_PROXY_UPSTREAMS` is resolved on the default upstream (public npm by default), which the proxy cannot tell from a public scope such as `@types`. If an operator forgets or mistypes a private scope, an attacker who published a package under that name on the public registry could get it installed (dependency confusion), install scripts included. List every private scope explicitly, and register the scope on the public registry when the organization owns it.
- **Tarball redirects to another host are refused.** The proxy follows a redirect only to a host that is itself a configured upstream, and answers `403` otherwise. A private registry that serves tarballs from another host (a CDN or object storage) will fail to install until that host is reachable some other way. Whether the registries in use redirect this way has not been checked here.
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
- **What is left out.** The pushed tree and the ZIP never contain `.git` or `node_modules` (at any depth), `dist` and `.orbit` at the project root, or environment files (`.env`, `.env.local`, `.env.production` and the like, in any letter case and at any depth), because they can hold a secret. `.env.example`, `.env.sample`, `.env.template` and `.env.defaults` are kept. A folder named `dist` below the root is source and is kept. Put values the app needs at build time in the deploy provider's environment settings, not in a file.
- **Failures.** Preconditions are checked before any side effect and answer `409` with a `reason` (`github_not_configured`, `deploy_not_configured`, `template_not_deployable`, `template_unavailable`, `nothing_to_publish`, `turn_in_progress`, `publish_in_progress`, `remote_moved`). If the branch was changed outside the builder (`remote_moved`), nothing is pushed and the remote is never overwritten. A first publish that fails after the code is pushed but before it is linked to the session archives the new repository and frontend app, so a retry starts clean. If the repository itself is gone (deleted, renamed or the GitHub installation lost access), the publish fails with `502`, not `remote_moved`. A Render failure after the code is on GitHub leaves the frontend app in place with a failed deploy status, and the existing deploy retry applies. The session is never modified by a failed publish.
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
- **Provider support.** `openai`, `openai_compatible` and `anthropic` are supported. A native Gemini adapter is not implemented. Gemini's OpenAI-compatible endpoint is documented by Google as beta; whether it handles tool calls correctly with this builder has not been verified here.
- **Anthropic provider.** The model's thinking blocks are not sent back between steps. Prompt caching is requested for the model calls of a turn (a request-level breakpoint with the default 5-minute entry), so each step reads the previous step's prompt at the cached-input price; it is not requested for the history compaction summary, a one-off call. The cache belongs to the workspace of the API key, and a step that starts more than 5 minutes after the previous one pays a new cache write. A model whose minimum cacheable prompt is larger than the prompt does not cache (cached input and cache write stay at zero in the usage report).
- **The planning model is the same provider as the build model.** A planning model from another provider is not supported: it would need two keys and two budgets. It is one setting per installation, not per user or session.
- **Prompt cache at the plan-to-build switch.** The tools offered differ between plan mode and build mode, so a planning call's cache was never reusable by the build calls. In one local check on Anthropic (one milestone, one run with and one without the setting), the first build call read nothing from the cache in both runs and wrote the same amount to it (within 1%), so setting a planning model added no cost at the switch.
- **Cost covers the builder only.** The "Build with AI" backend chat is not recorded in the usage report.
- **Publishing needs GitHub and Render.** Other publish targets are not supported.
- **No automatic backend changes and no publish check on open tables yet.** The settings `backend_approval_mode` and `publish_rls_gate` exist in `GET /dashboard/api/builder/settings`, but only their defaults are accepted (`ask` and `warn`): `PUT` answers `400` for `auto` and `block`. Every backend change waits for the user, and publishing does not list or refuse tables that anyone can read; review the access of each table before publishing.
- **Reverting does not revert the brief.** Restoring an earlier turn keeps every brief version and the milestone progress. `ORBIT_BRIEF.md` is restored with the code, and the next build turn writes the current brief again.
- **Question rounds per turn.** The limit of question rounds counts per turn. If a planning turn ends early (a budget or an error), the next message starts with a new allowance.
- **Planning and real providers.** A plan-first turn in a session that already built has calls to write tools in its history while those tools are not offered. Both adapters send that history unchanged; how each real provider endpoint handles it has been checked against test doubles only.

## Troubleshooting

`GET /dashboard/api/builder/status` returns `{"available": false, "provider_configured": ..., "reason": "..."}`. The dashboard shows a message based on the `reason`. The checks run in this order, and the first failing one is reported.

| `reason` | Meaning | What to check |
|----------|---------|---------------|
| `not_configured` | Orbit has no builder wired: `BUILDER_URL` is empty. | Set `BUILDER_URL` and `BUILDER_SHARED_SECRET` on every Orbit replica and restart. In Compose, start with `--profile builder`. In Helm, set `builder.enabled=true`. With Kustomize, check that the Component is included in the overlay. |
| `disabled` | The builder is wired, but **Enable AI Builder** is off in the settings. | A superadmin turns it on in **Integrations**, **AI Builder** tab. |
| `provider_not_configured` | The model provider chosen in the builder settings is unusable. | In the **AI** tab, check the provider row: it must be enabled and have a model and, for `openai`, an API key; for `openai_compatible` it needs a base URL; for `anthropic`, an API key. |
| `model_not_in_catalog` | The `openai` or `anthropic` provider row holds a model that is not in Orbit's model catalog, so its calls could not be priced. | In the **AI** tab, pick a model from the list and save. |
| `budget_not_configured` | A token cap is `0` (unset), so every turn would end with `budget_exceeded`. | In the **AI Builder** tab, set both the per-session and the per-user monthly token caps to a value greater than zero. Until then the builder is unavailable and **Create with AI** is disabled. |
| `builder_unreachable` | Orbit could not get a healthy answer from the builder. | Check that the builder container or pod is running, that `BUILDER_URL` resolves from Orbit, and that the network (Compose network, NetworkPolicy ingress from the Orbit pods on port 8081) lets Orbit through. A different `BUILDER_SHARED_SECRET` on the two sides makes every request fail with `401`, which also shows up as unreachable. The builder health endpoint requires a signed token, so a plain `curl` to it answers `401`; use the builder's logs instead. |

Other symptoms:

- **A turn ends right away with `budget_exceeded`.** A budget was reached or was set to `0` while the turn was running. The chat names the budget that stopped the turn. Check the session and monthly token budgets (both must be greater than zero) and, if set, the cost budgets.
- **The cost reads "no price set".** For an OpenAI-compatible model, it has no row in the **Model prices** card: add one. For OpenAI or Anthropic, the model is not in the catalog: pick a catalog model in the **AI** tab. Calls already recorded keep no cost.
- **A turn stays `queued`.** The builder is at capacity (`BUILDER_MAX_ACTIVE_SESSIONS`), or Orbit replicas are running their maximum number of turns (`BUILDER_WORKER_CONCURRENCY`). It starts when a slot frees up.
- **A turn ends with a generic error.** Users see only a generic message by design; provider and builder error text is logged on the Orbit server, not returned. Look in Orbit's server logs.
- **`npm install` fails with `E404` for a private package.** See the scoped-registry limitation under [Isolation model](#isolation-model), and check `REGISTRY_PROXY_UPSTREAMS` and the matching `REGISTRY_PROXY_TOKEN_<NAME>`.
- **Preview shows `401`.** The preview token expired (10 minutes) or belongs to another user or session. Reload the session page.
