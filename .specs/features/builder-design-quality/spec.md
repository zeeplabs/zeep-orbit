# AI Builder Design Quality Specification

> Status: **Q1 and Q3 answered 2026-11-23, Q2 answered as a finding (screenshots are a separate stage). Ready for design.** Child of `.specs/features/ai-frontend-builder/` and `.specs/features/ai-builder-skills/`. Read the skills spec §Assumptions 6, 7 and 9 first: precedence and the built-in `orbit-frontend-design` skill are defined there.

## Problem Statement

The builder produces generic, recognizably AI-made interfaces on the first pass. Getting a good one takes a second message from the user that spells out the ambition ("professional, out of the curve, animations, clean, beautiful"), and the result then looks good but the quality is not the default. Observed in beta.2 testing on 2026-11-23; the same symptom was already recorded on 2026-10-27 (skills spec, Problem Statement).

Causes found in the code (2026-11-23, not hypotheses):

1. `internal/agent/prompts/core.md` says nothing about visual quality. The only UI line is about language.
2. The built-in skill `orbit-frontend-design` exists but loads only when the model calls `load_skill`. In a plain "build a screen" turn the model may never call it.
3. When loaded, the skill pushes toward restraint ("no entrance animations on every section", "one bold element, the rest quiet") and states "you cannot see the result". It prevents the generic look but does not ask for a distinctive one.
4. The `default` template is plain CSS with no UI kit, and its `ORBIT_AGENT.md` says "No Tailwind or UI kit" and "no new dependencies unless the task cannot be done without them". Every app starts from the same bare page.
5. The plan has an optional `Look` (style, colors, interface language, reference; `internal/agent/planning/planning.go`). Nothing forces a visual direction to be decided, and nothing derives it from the kind of project.
6. The agent has no way to see what it built (no render or screenshot step in the loop).

## Goals

- [ ] A first-pass interface, with no extra prompting from the user, reads as designed for its project type, not as a generic template. Measured by a visual eval (DQ-20), not by opinion.
- [ ] Two apps of different kinds built by the same Orbit install do not share the same look (palette, type, density, motion).
- [ ] When the template declares a UI kit, the agent builds with that kit's components and tokens and does not hand-roll equivalents.
- [ ] When the template declares no kit, the agent uses the Orbit default component library and adds only the components the library lacks.
- [ ] Apps built by Orbit share one code pattern (folder layout, component API, token names), so they are readable and maintainable across projects.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Pixel-perfect or brand-exact design from a reference site or image | Needs image input and a review loop; separate feature. |
| Letting the end user pick among visual presets in the UI | The audience is non-technical; the agent decides and the plan lets the user correct it (DQ-03). |
| Loading fonts, icons or assets from third-party hosts | Already forbidden by `orbit-frontend-design`: preview and published apps must not call external hosts. |
| Replacing company standards or the template guide | Precedence stays CORE > company > template guide > skills (skills spec, Assumption 9). |
| Re-skinning apps already built | Applies to new turns and new apps. A restyle request still goes through the normal flow. |
| Marketplace or per-user component libraries | No demand; single maintainer. |

---

## Assumptions & Open Questions

| # | Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- | --- |
| 1 | Do not encode the ambition as adjectives ("beautiful, wow") | Encode it as craft rules: decide a visual direction first, write it as tokens, give motion a purpose, finish every state. | Adjectives yield a different generic (gradients, glass, fade-in everywhere); `orbit-frontend-design` already lists these as tells. | proposal |
| 2 | Variety comes from tokens, structure comes from the library | The component library fixes structure, behavior and accessibility. Palette, type pairing, radius, density and motion come from per-project tokens. | A shared library with a shared theme would make every app identical, which contradicts the goal. | proposal |
| 3 | Default library form | shadcn-style on **Tailwind** (user, Q1): components copied into the app (`src/components/ui`), tokens as CSS variables mapped through Tailwind's theme. Runtime packages (the small set shadcn components need) go through `registry-proxy`; none is a component package. | The generated app stays self-contained and the dashboard already uses shadcn. | y (user, 2026-11-23) |
| 4 | Where the visual direction is decided | In planning, as a required, richer `Look`, approved by the user with the rest of the plan, and kept in the app brief (stable prompt prefix). | The user can correct it before any code is written; no new prompt section is needed. | proposal |
| 5 | Template declares its UI kit explicitly | New optional `ui_kit` object in `orbit-template.json` (name, component path, usage notes file). Not inferred from `package.json`. The builder reads it from the session workspace each turn, like `ORBIT_AGENT.md`, so it works for the default template and for admin-registered GitHub templates alike. | Inference from dependencies is fragile and would silently misfire on unrelated packages. | proposal |
| 5b | Which template a session follows (user, Q3) | A session follows the template it was started from. An admin-registered template (GitHub template with `builder_enabled`) is followed as it is: its kit, guide and tokens win and the Orbit library is **not** injected into it. The `default` template carries the Orbit library, so every new project without a custom template gets it. | One rule, no merge of two design systems. A custom template without `ui_kit` keeps whatever it has; the agent then follows its guide and only the skill applies. | y (user, 2026-11-23) |
| 6 | Precedence with a kit | Company standards > template guide / `ui_kit` > Orbit default library > built-in skills. The default library is used only when no `ui_kit` is declared. | Matches the user's request and the existing header order. | y (user, 2026-11-23) |
| 7 | Visual feedback | A render-and-look step is the largest quality lever but needs infrastructure that does not exist today (see Q2 findings). Delivered as its own stage (P3), as a sidecar service, after a spike. | Findings below. | proposal |
| 8 | Prompt cost | The always-on additions to core and brief must stay small; the visual guidance lives in skills loaded on demand, but the core states when it must be loaded. | Core is paid on every call (skills spec, Problem Statement). | proposal |

**Answered questions:**

- **Q1 (user):** Tailwind. Consequence: the template build gains `tailwindcss` and its Vite plugin plus the packages shadcn-style components need; all come from the npm upstream configured in `registry-proxy` (default `registry.npmjs.org`). Exact pinned versions are chosen and checked at implementation time (the template pins exact versions today).
- **Q3 (user):** see Assumption 5b.
- **Q2 (user: "I don't know what it would take"). Findings from the code, 2026-11-23:**
  - The builder container is hardened: read-only root, `cap_drop: ALL`, `pids_limit: 512`, `mem_limit: 4g`, uid 1000, and its only way out is `registry-proxy` (`docker-compose.yml`, `Dockerfile.builder`). Chromium inside it would need either `--no-sandbox` (weakens isolation for untrusted generated code) or user namespaces that `cap_drop`/seccomp block. Not recommended there.
  - The image is `node:22-bookworm-slim`; a browser adds hundreds of MB and system libraries.
  - The provider layer has **no image support** (no image or vision handling in `internal/agent/provider`) and the model catalog has no vision flag. The model must receive the screenshot, so adapters, tool-result format and catalog all need to change, and only multimodal models can use the step.
  - The preview is already a static build served through the builder (`builder_preview.go`), so a renderer only needs the `dist` output, not a dev server.
  - Recommended shape: a **separate sidecar** (its own container on the internal network, no access to Orbit or the internet) that receives a built `dist`, serves it locally, renders it with a headless browser and returns a PNG. It never runs inside the builder. Capturing in the user's browser was considered and rejected: the preview iframe has an opaque origin, and a turn must not depend on the user keeping the tab open.
  - Not verified: which configured models accept images, and the cost of an image per turn. These are the first things the spike measures.

---

## User Stories

### P1 (MVP): good by default, no template or dependency change

**US-1.** As a user who describes an app in one message, I want the first interface to look designed for my project, so I do not have to ask for "something professional" afterwards.
**US-2.** As a user, I want to see and correct the intended visual direction in the plan before the build starts.

### P2: component library and template kit

**US-3.** As a template author, I want to declare a UI kit so the builder uses my components and tokens.
**US-4.** As an Orbit operator, I want apps without a kit to start from one default component library, so projects share a code pattern.

### P3: the agent checks its own result

**US-5.** As a user, I want the builder to look at what it built and fix obvious visual problems before it tells me it is done.

---

## Requirements

Priority: P1 = MVP, P2 = next, P3 = after feasibility.

### Visual direction (P1)

| ID | Requirement | Priority |
| --- | --- | --- |
| DQ-01 | `Look` is required in a proposed plan (plan validation rejects an empty look with an actionable message). | P1 |
| DQ-02 | `Look` carries a visual direction chosen for the project type: mood, palette intent, type pairing, density, and where motion goes. Fields stay short; the rendered brief still fits `MaxBriefChars`. | P1 |
| DQ-03 | The visual direction is shown to the user in the plan card and can be edited like any other part of the plan. | P1 |
| DQ-04 | The planner is instructed to derive the direction from the audience and job of the app, and to avoid repeating the previous generic choice (evidence: distinct directions across the eval briefs). | P1 |
| DQ-05 | The direction is written as tokens in `src/index.css` at the first build step (the brief says so; the skill describes how). | P1 |

### Core and skill (P1)

| ID | Requirement | Priority |
| --- | --- | --- |
| DQ-06 | `core.md` gets a short rule: for any new screen or restyle, load `orbit-frontend-design` first and follow the plan's `Look`. | P1 |
| DQ-07 | `orbit-frontend-design` is rewritten from "restraint" to "intent": a decision step, a range of directions, purposeful motion (page entry, state change, feedback), full state coverage, and the existing anti-tells. It still yields to company standards, the template guide and the kit. | P1 |
| DQ-08 | Motion is on by default where it has a purpose and always respects `prefers-reduced-motion`. | P1 |
| DQ-09 | The skill index description of `orbit-frontend-design` is tuned so it matches "build or change a screen" tasks (eval: loaded in the positive tasks, not in the negative ones). | P1 |

### Template UI kit (P2)

| ID | Requirement | Priority |
| --- | --- | --- |
| DQ-10 | `orbit-template.json` accepts an optional `ui_kit` (name, components path, usage notes path). Validation rejects unknown fields and paths outside the template. | P2 |
| DQ-11 | When `ui_kit` is present, the prompt states the kit in the template section and the agent imports its components instead of writing equivalents; creating a component is allowed only when the kit lacks it, and it follows the kit's tokens. | P2 |
| DQ-12 | Usage notes of the kit (component list, props, tokens) are available on demand (`load_skill`-style or `read_file`), not pasted whole into the prompt (cap: `MaxTemplateGuideChars`). | P2 |
| DQ-13 | Eval: with a kit-bearing fixture template, generated screens import from the kit path and do not redefine buttons, inputs, dialogs or tables. | P2 |

### Default component library (P2)

| ID | Requirement | Priority |
| --- | --- | --- |
| DQ-14 | The `default` template ships a component library (button, input, select, textarea, checkbox, dialog, table, card, tabs, toast, empty state, skeleton at minimum) with accessible behavior and CSS-variable theming. | P2 |
| DQ-15 | The library is declared through the same `ui_kit` mechanism as a third-party kit, so the agent has one rule, not two. | P2 |
| DQ-16 | Theming is token-only: a new app changes tokens, not component source, to change its identity. Two fixture apps with different `Look` render visibly different (eval, DQ-20). | P2 |
| DQ-17 | The library's packages come only from the configured `registry-proxy` upstream, pinned to exact versions; `npm ci`, `npm run build` and `npm run lint` stay green and first-install time is measured against the current template. | P2 |
| DQ-18 | The template guide documents the library and the "kit first, then extend" rule; `TEMPLATE_VERSION` is bumped. Existing apps are not migrated: the library applies to new projects only (Assumption 5b). | P2 |

### Visual feedback and evaluation (P3 / cross-cutting)

| ID | Requirement | Priority |
| --- | --- | --- |
| DQ-19 | Feasibility spike for a render-and-look step (Q2). Output: yes/no, cost per turn, and where it runs. Nothing is built before the answer. | P3 |
| DQ-20 | A visual eval: a fixed set of briefs rendered to screenshots, scored by rubric (hierarchy, distinctiveness between project types, kit usage, contrast, state coverage). Baseline recorded before any change so improvement is measurable. | P1 (rubric and baseline), P3 (automation) |
| DQ-21 | If DQ-19 is yes, the loop gets a bounded screenshot step (max iterations and cost cap reuse the existing budget) and the agent fixes issues it can name. | P3 |

---

## Success Criteria

- Baseline recorded first (DQ-20). After P1, blind comparison of first-pass outputs for the fixed briefs: reviewer prefers the new output in a clear majority, and no two different project types share a palette and type pairing. Thresholds fixed in design once the baseline exists; no number is promised here.
- After P2, 100% of screens in the kit fixtures import from the declared kit path (DQ-13).
- Prompt growth from P1 stays within a budget set in design (core change is a few lines; no new always-on section).
- No regression in the planning eval (shape test of the 12 briefs) and in the language behavior fixed in `fix/builder-reply-language`.

## Risks

| Risk | Impact | Mitigation |
| --- | --- | --- |
| "Beautiful by default" becomes a new uniform style (every app gets the same bold look) | Goal missed; users notice | Directions are a range tied to project type; eval checks distinctiveness between types (DQ-16, DQ-20). |
| Motion hurts accessibility or performance | Compliance and UX | DQ-08; the accessibility skill stays loaded for interactive UI. |
| Default library inflates the template and the build time | Slower first build, higher cost | Measure in design; start with the minimum set (DQ-14); budget per turn unchanged. |
| Registry or offline limits block library dependencies | Build fails in the sandbox | Pinned exact versions, registry-proxy only, minimal package set, install time measured (DQ-17). |
| Without screenshots, quality gains plateau | P1 and P2 help less than expected | DQ-19 spike decides P3; DQ-20 baseline shows the plateau. |
| Template kit and company standards disagree | Inconsistent output | Precedence in Assumption 6; the company layer wins. |

## Traceability

| Requirement | Design | Tasks | Status |
| --- | --- | --- | --- |
| DQ-01..DQ-05 | not started | not started | Specified |
| DQ-06..DQ-09 | not started | not started | Specified |
| DQ-10..DQ-13 | not started | not started | Specified |
| DQ-14..DQ-18 | not started | not started | Specified |
| DQ-19..DQ-21 | not started | not started | Specified |
