# AI Builder Design Quality Design

**Spec**: `.specs/features/builder-design-quality/spec.md`
**Status**: Draft 2026-11-23. Facts below were read from the code on that date; anything not read is marked `[not verified]`.

---

## 1. Where the pieces live today

| Piece | Where | Relevant behavior |
| --- | --- | --- |
| Core rules | `internal/agent/prompts/core.md`, embedded, never truncated | No visual-quality rule. |
| System prompt | `internal/agent/prompt.go` `BuildSystemPromptWithBrief` | Order: core, company, template guide (cap 12000), skills index (cap 8000), brief (cap `MaxBriefChars` 6000), session context. |
| Template guide | `internal/dashboard/builder_runtime.go` `scanWorkspaceFiles` | Reads `ORBIT_AGENT.md` from the session's committed workspace each turn, so it works for any template, including GitHub templates. |
| Plan and brief | `internal/agent/planning` (`Look`: style, colors, ui_language, reference, all optional), `RenderBrief` | `Look` renders as one `Look:` line in the brief. |
| Built-in skill | `internal/agent/skills/builtin/orbit-frontend-design/SKILL.md` | Loaded only through `load_skill`; text pushes restraint; says the agent cannot see the result. |
| Default template | `builder/templates/default` (plain CSS, no kit; guide says "No Tailwind or UI kit") | Copied into `/opt/templates/default` in the builder image. |
| Template metadata | `internal/dashboard/builder_templates.go` | The dashboard does **not** read `orbit-template.json`; defaults are duplicated as constants and must be kept in sync by hand. Admin templates are GitHub templates with `builder_enabled`. |
| Registry | `cmd/zeep-registry-proxy`, `REGISTRY_PROXY_UPSTREAMS` (default `registry.npmjs.org`) | The only way out of the builder container. |
| Preview | `internal/dashboard/builder_preview.go` | Serves the session's static build under a token path. |

## 2. P1: good by default

### 2.1 Required, richer `Look` (DQ-01..DQ-05)

`planning.Look` gains short fields and becomes required:

```go
type Look struct {
    Style      string `json:"style,omitempty"`     // existing
    Colors     string `json:"colors,omitempty"`    // existing
    UILanguage string `json:"ui_language,omitempty"` // existing
    Reference  string `json:"reference,omitempty"`   // existing
    Direction  string `json:"direction,omitempty"`   // new: one named direction, e.g. "editorial", "dense-operational", "playful", "calm-clinical", "bold-product"
    Type       string `json:"type,omitempty"`        // new: type pairing/feel, system-font safe
    Density    string `json:"density,omitempty"`     // new: airy | balanced | compact
    Motion     string `json:"motion,omitempty"`      // new: where motion goes and why
}
```

- Same `field()` validation as the others, `maxLookFieldChars` 200 each. `(*Plan).RequireLook()` rejects a proposal whose `Look.Direction` is empty (DQ-01); the `propose_build_plan` handler calls it after `ParsePlan`. `ParsePlan` itself stays tolerant because it also reads stored briefs (`builder_runtime.go` `briefInput`), which have no direction. The user's own style and colors alone do not satisfy it: the direction is the planner's decision. The tool schema lists `look` and `look.direction` as required.
- `lookText` renders the new fields. Budget: the worst case adds about 4 x 200 characters to a brief that is capped at 6000; the existing "plan too long" error already protects the cap. **To measure in T1:** brief size of the 12 eval briefs before and after.
- `proposeBuildPlanDescription` and the planning prompt tell the planner to choose the direction from the audience and job of the app, and not to reuse one direction for every project (DQ-04). The directions listed are examples, not an enum: free text, so a company can say "our brand".
- Frontend: the plan card shows the direction and lets the user edit it (DQ-03). It renders from the plan JSON, so no new endpoint. `[not verified]` how the plan card lists `Look` today: read `internal/dashboard/ui` plan components in T3.

### 2.2 Core rule (DQ-06)

One short paragraph in `core.md` (target: under 600 characters, no new section):

> For any new screen or restyle, load `orbit-frontend-design` first, then build the plan's Look as tokens in the stylesheet before writing components. Company standards, the template guide and the template's UI kit take precedence over it.

`TestBuildSystemPrompt_*` gets a test that pins the sentence (same pattern as the language rule).

### 2.3 Skill rewrite (DQ-07..DQ-09)

`orbit-frontend-design` version 2 (`version:` bump; `builtin.lock` hash updated; description stays within `MaxBuiltinDescriptionChars` 200):

- Keeps: the decision step, tokens first, hierarchy rules, contrast floor, anti-tells, copy rules, quality floor, "yield to company/template/kit".
- Changes from "restraint" to "intent": a short table of directions with what each implies for type, density, shape and motion; "motion with a purpose" (page entry, state change, user-action feedback) with durations/easing guidance and `prefers-reduced-motion`; every state designed (loading, empty, error, success).
- Removes "you cannot see the result" wording once P3 exists; until then keeps it and says to keep CSS simple.
- Adds a "when a kit is present" section: use its components and tokens; extend only what is missing.
- Description rewritten to match task wording ("build or change a screen, restyle, make it look better"), checked by the skills trigger eval (measured manually from the session timeline: the parent T42 runner does not exist, see tasks T6 and T14).

### 2.4 Baseline and rubric (DQ-20)

Before T2..T6 change anything: run 5 of the 12 planning-eval briefs (different project kinds) on the current prompt in the local beta stack with one reference model, save a screenshot per build, and score with the rubric in `validation.md` (created in T1): hierarchy, distinctiveness between project types, state coverage, contrast, kit usage. Automation waits for P3.

## 3. P2: template UI kit and default library

### 3.1 `ui_kit` declaration (DQ-10..DQ-12)

`orbit-template.json` (unchanged fields kept):

```json
{
  "version": "1",
  "render_service_type": "static_site",
  "build_command": "npm ci && npm run build",
  "publish_path": "dist",
  "ui_kit": {
    "name": "orbit-ui",
    "components": "src/components/ui",
    "tokens": "src/index.css",
    "notes": "UI_KIT.md"
  }
}
```

- Read where the guide is read: `scanWorkspaceFiles` also captures `orbit-template.json` and `UI_KIT.md` (paths from the manifest, validated as relative, no `..`, regular files in the tree). New `workspaceScan.kit` struct. Unknown keys inside `ui_kit` are rejected with a warning in the log and the kit ignored (a bad declaration must not block a turn). This avoids changing the dashboard's duplicated template constants.
- Prompt: when a kit is present, `BuildSystemPromptWithBrief` adds a fixed short block under the template header: kit name, components path, "import from here; create a component only when the kit lacks one; follow the tokens file". The block is data derived from the manifest, not free text from the template, so a template cannot inject instructions through it. Its `notes` file is **not** pasted: the model reads it with `read_file` (DQ-12), and the file tree already lists the components.
- GitHub (admin) templates: same file names, so an admin adds `orbit-template.json` + `UI_KIT.md` + `ORBIT_AGENT.md` to the template repo. Document this in `docs/` (T11).

### 3.2 Default library in the `default` template (DQ-14..DQ-18)

- Build: Tailwind with its Vite plugin; tokens live in `src/index.css` through Tailwind's CSS-first theme variables, so the "write tokens first" rule (DQ-05) edits one file. `[not verified]` exact Tailwind major to pin; choose the current stable at T8 with `npm view` through the proxy and pin exactly.
- Components copied into `src/components/ui`, shadcn-style: button, input, textarea, select, checkbox, label, dialog, table, card, tabs, toast, empty-state, skeleton, badge. Accessibility behavior comes from small headless primitives; which packages, and whether to hand-write the dialog/select, is decided in T8 by counting installed size. Rule: no component package that ships its own styling.
- Icons: a bundled icon package only (no CDN), per the existing "no external hosts" rule `[not verified]` which package is allowed by the proxy upstream; confirm in T8.
- Theming: components read semantic tokens (`--background`, `--foreground`, `--primary`, `--radius`, `--font-sans`, spacing and motion tokens). The `Look` direction maps to token values; no component source change is needed to change identity (DQ-16).
- `ORBIT_AGENT.md` of the template is rewritten: stack now includes Tailwind and the library; "no UI kit" line removed; "kit first, then extend" added; dependency rule says Tailwind and the listed library packages are already installed.
- `TEMPLATE_VERSION` bumped; the default template constants in `builder_templates.go` stay valid (`npm ci && npm run build`, `dist`).
- Risks to measure in T8: first `npm ci` time and size through the proxy, `npm run build` time, lint rules (the template's ESLint config must accept the new files).

### 3.3 Precedence (Assumption 5b, 6)

Already encoded by section order and headers: core > company > template guide (includes the kit block) > skills. The default library is not a separate layer: it **is** the `default` template's kit, so there is one rule. An admin template without `ui_kit` gets no library injection.

## 4. P3: visual feedback (spike first)

Not designed in detail until the spike answers. Spike scope (T12): a sidecar container with a headless browser on the builder's internal network, given a built `dist` (tar from the existing `Archive` call), serving it locally and returning a PNG.

Questions the spike must answer, with numbers:

1. Image support: which configured models accept images; what the provider adapters need (tool-result image blocks) and the catalog flag.
2. Cost and latency of one screenshot turn on the reference models.
3. Isolation: sidecar has no route to Orbit or the internet, read-only root, `cap_drop: ALL`; browser sandbox on or off, with the reason.
4. Image size and number of captures that keep a turn inside the budget caps.

Decision output: go/no-go and a design addendum (§4.1+). If go, the loop adds a bounded tool `look_at_preview` (read-only, counted against the same budget). No code before the addendum.

## 5. Testing strategy

| Change | Test |
| --- | --- |
| `Look` fields, validation, `lookText` | unit in `internal/agent/planning` (accept/reject, size cap with the worst case). |
| Core and kit prompt block | unit in `internal/agent/prompt_test.go` (sentence pinned, kit block present/absent, injection attempt in manifest fields ignored). |
| `scanWorkspaceFiles` kit capture | unit with tar fixtures: valid kit, `..` path, missing file, unknown key, oversized manifest. |
| Skill v2 | existing skill lint tests (`lint_test.go`) and `builtin.lock` check. |
| Template build | CI-style: `npm ci && npm run build && npm run lint` on the template (new check, T8). |
| Quality | visual eval (DQ-20) and kit eval (DQ-13): manual on the local stack with a fixed rubric until T14 (build-level eval runner, optional) exists. The existing planning eval (`-tags planningeval`) already measures the plan stage with a real provider and can be reused for `Look` distinctness. |

## 6. Decisions to confirm at implementation

- Tailwind major and the exact package list (T8).
- Whether `Look.Direction` should offer suggested values in the plan card UI or stay free text (T3).
- Where the baseline screenshots are stored (repo `.specs/.../baseline/` vs. outside the repo) given size.
